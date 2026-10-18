// Strata is a small, self-hosted file manager for a TrueNAS share.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"strata/internal/auth"
	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/sheet"
	"strata/internal/state"
	"strata/internal/thumb"
	"strata/internal/tlsutil"
	"strata/internal/userr"
	"strata/internal/web"
)

//go:embed all:dist
var embedded embed.FS

// maxUpload caps a single uploaded file. NAS users move large media, so this
// is generous; the reverse proxy should carry a matching limit.
const maxUpload = 8 << 30 // 8 GiB

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

var (
	root     *safepath.Root
	thumbs   *thumb.Cache
	appState *state.Store // nil without a config volume
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck(envOr("STRATA_ADDR", ":8080")))
		case "hash-password":
			os.Exit(hashPassword())
		case "version", "--version":
			fmt.Println(version)
			return
		}
	}

	var (
		dir  = flag.String("root", envOr("STRATA_ROOT", "/data"), "directory to serve")
		addr = flag.String("addr", envOr("STRATA_ADDR", ":8080"), "listen address")
		name = flag.String("name", envOr("STRATA_NAME", "Strata"), "display name")
		// Hostnames (no port) the UI is reached by, e.g. strata.example.com.
		// IP addresses and localhost always work; anything else is refused.
		hosts = flag.String("allowed-hosts", envOr("STRATA_ALLOWED_HOSTS", ""), "comma-separated hostnames")
		// File holding the argon2id hash from `strata hash-password`.
		pwFile = flag.String("password-file", envOr("STRATA_PASSWORD_FILE", ""), "passphrase hash file")
		// Proxies (IPs or CIDRs) whose X-Forwarded-* headers are believed.
		proxies  = flag.String("trusted-proxies", envOr("STRATA_TRUSTED_PROXIES", ""), "comma-separated proxy IPs/CIDRs")
		authMode = flag.String("auth", envOr("STRATA_AUTH", "passphrase"), `"passphrase" or "none"`)
		// Persistent state: passphrase hash, TLS pair, app state.
		cfgDir  = flag.String("config", envOr("STRATA_CONFIG", ""), "config directory (default /config if present)")
		tlsMode = flag.String("tls", envOr("STRATA_TLS", "auto"), `"auto", "on" or "off"`)
		tlsCert = flag.String("tls-cert", envOr("STRATA_TLS_CERT", ""), "TLS certificate (PEM)")
		tlsKey  = flag.String("tls-key", envOr("STRATA_TLS_KEY", ""), "TLS private key (PEM)")
		// Days a deleted item stays restorable.
		trashDays = flag.Int("trash-days", envInt("STRATA_TRASH_DAYS", 30), "days to keep deleted items")
	)
	flag.Parse()

	r, err := safepath.NewRoot(*dir)
	if err != nil {
		log.Fatalf("root %q unusable: %v", *dir, err)
	}
	root = r
	log.Printf("strata %s serving %s on %s", version, root.Path(), *addr)

	config, err := configDir(*cfgDir)
	if err != nil {
		log.Fatal(err)
	}

	gate, err := loadGate(*authMode, *pwFile, config, strings.Split(*proxies, ","))
	if err != nil {
		log.Fatal(err)
	}

	// Behind a TLS-terminating proxy, "auto" means plain HTTP on the
	// internal network; anywhere else it means HTTPS.
	mode := *tlsMode
	if mode == "auto" && *proxies != "" && *tlsCert == "" {
		mode = "off"
		log.Printf("TLS off: behind trusted proxy %s (set STRATA_TLS=on to force HTTPS)", *proxies)
	}
	tlsCfg, fp, err := tlsutil.Config(mode, *tlsCert, *tlsKey, config, strings.Split(*hosts, ","))
	if err != nil {
		log.Fatalf("TLS: %v", err)
	}

	if err := openCaches(config); err != nil {
		log.Fatal(err)
	}

	go sweep(max(*trashDays, 1))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(newHandler(*name, strings.Split(*hosts, ","), gate)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		// No write timeout: large downloads and uploads legitimately run long.
	}
	if tlsCfg == nil {
		log.Fatal(srv.ListenAndServe())
	}
	srv.TLSConfig = tlsCfg
	log.Printf("HTTPS on; certificate SHA-256 fingerprint %s", fp)
	log.Fatal(srv.ListenAndServeTLS("", ""))
}

// configDir picks the directory for persistent state. An explicit setting
// must be usable; the default /config is optional, since a plain compose
// deployment may run without one.
func configDir(explicit string) (string, error) {
	dir := explicit
	if dir == "" {
		dir = "/config"
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			log.Printf("no config volume at %s; first-run setup and saved state are off", dir)
			return "", nil
		}
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return "", fmt.Errorf("config directory %s is not writable: %w", dir, err)
	}
	probe.Close()
	os.Remove(probe.Name())
	return dir, nil
}

// openCaches sets up app state and the thumbnail cache. Without a config
// volume, thumbnails use a small scratch cache and nothing is remembered.
func openCaches(config string) error {
	dir, limit := filepath.Join(tlsutil.ScratchDir(), "thumbs"), int64(32<<20)
	if config != "" {
		s, err := state.Open(config)
		if err != nil {
			return fmt.Errorf("state: %w", err)
		}
		appState = s
		dir, limit = filepath.Join(config, "thumbs"), 500<<20
	}
	c, err := thumb.New(dir, limit)
	if err != nil {
		log.Printf("thumbnails off: %v", err)
		return nil
	}
	thumbs = c
	return nil
}

// loadGate fails closed: without a usable passphrase hash Strata refuses to
// start, unless auth was switched off explicitly.
func loadGate(mode, pwFile, config string, proxies []string) (*auth.Gate, error) {
	switch mode {
	case "none":
		log.Printf("WARNING: STRATA_AUTH=none, anyone who can reach this port can read and delete every file")
		return nil, nil
	case "passphrase":
	default:
		return nil, fmt.Errorf("STRATA_AUTH must be \"passphrase\" or \"none\", got %q", mode)
	}
	if pwFile == "" && config != "" {
		pwFile = filepath.Join(config, "auth.hash")
		if _, err := os.Stat(pwFile); errors.Is(err, os.ErrNotExist) {
			g, code, err := auth.NewSetupGate(pwFile, proxies)
			if err != nil {
				return nil, err
			}
			log.Printf("================================================================")
			log.Printf("  FIRST-RUN SETUP CODE: %s", code)
			log.Printf("  Open Strata in a browser and enter this code to set a passphrase.")
			log.Printf("  A new code is printed on every start until setup is done.")
			log.Printf("================================================================")
			return g, nil
		}
	}
	if pwFile == "" {
		return nil, errors.New("no passphrase configured: mount a config volume at /config for first-run setup, " +
			"or run `strata hash-password` and point STRATA_PASSWORD_FILE at the result " +
			"(or set STRATA_AUTH=none for a trusted network)")
	}
	info, err := os.Stat(pwFile)
	if err != nil {
		return nil, fmt.Errorf("password file: %w", err)
	}
	if info.Mode().Perm()&0o004 != 0 {
		log.Printf("WARNING: %s is world-readable; restrict it to the strata user", pwFile)
	}
	raw, err := os.ReadFile(pwFile)
	if err != nil {
		return nil, fmt.Errorf("password file: %w", err)
	}
	v, err := auth.NewVerifier(string(raw))
	if err != nil {
		return nil, fmt.Errorf("password file %s: %w", pwFile, err)
	}
	return auth.NewGate(v, proxies)
}

// hashPassword prints an argon2id hash for STRATA_PASSWORD_FILE. On a
// terminal it prompts twice without echo; otherwise it reads one line.
func hashPassword() int {
	var pass string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Passphrase: ")
		a, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprint(os.Stderr, "Again: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil || string(a) != string(b) {
			fmt.Fprintln(os.Stderr, "passphrases do not match")
			return 1
		}
		pass = string(a)
	} else {
		line, err := bufio.NewReader(io.LimitReader(os.Stdin, 4<<10)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		pass = strings.TrimRight(line, "\r\n")
	}
	h, err := auth.Hash(pass)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(h)
	return 0
}

// newHandler builds the routes behind the request guards. A nil gate means
// auth is off.
func newHandler(name string, hosts []string, gate *auth.Gate) http.Handler {
	mux := http.NewServeMux()

	if gate != nil {
		mux.HandleFunc("POST /api/login", gate.Login)
		mux.HandleFunc("POST /api/setup", gate.Setup)
		mux.HandleFunc("POST /api/logout", gate.Logout)
		mux.HandleFunc("GET /api/session", gate.Session)
	} else {
		mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"ok": true, "auth": false})
		})
	}

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "name": name})
	})
	mux.HandleFunc("GET /api/list", handleList)
	mux.HandleFunc("GET /api/usage", handleUsage)
	mux.HandleFunc("GET /api/download", handleDownload)
	mux.HandleFunc("GET /api/raw", handleRaw)
	mux.HandleFunc("GET /api/text", handleText)
	mux.HandleFunc("POST /api/text", handleTextWrite)
	mux.HandleFunc("GET /api/thumb", handleThumb)
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/favorite", handleFavorite)
	mux.HandleFunc("POST /api/recent", handleRecent)
	mux.HandleFunc("GET /api/sheet", handleSheetRead)
	mux.HandleFunc("POST /api/sheet", handleSheetWrite)
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("POST /api/uploads", handleUploadStart)
	mux.HandleFunc("HEAD /api/uploads/{id}", handleUploadOffset)
	mux.HandleFunc("PUT /api/uploads/{id}", handleUploadChunk)
	mux.HandleFunc("POST /api/uploads/{id}/finish", handleUploadFinish)
	mux.HandleFunc("DELETE /api/uploads/{id}", handleUploadAbort)
	mux.HandleFunc("GET /api/search", handleSearch)
	mux.HandleFunc("GET /api/zip", handleZip)
	mux.HandleFunc("POST /api/mkdir", handleMkdir)
	mux.HandleFunc("POST /api/move", handleMove)
	mux.HandleFunc("POST /api/copy", handleCopy)
	mux.HandleFunc("POST /api/delete", handleDelete)
	mux.HandleFunc("GET /api/trash", handleTrashList)
	mux.HandleFunc("POST /api/trash/restore", handleTrashRestore)
	mux.HandleFunc("POST /api/trash/purge", handleTrashPurge)
	mux.HandleFunc("GET /api/versions", handleVersions)
	mux.HandleFunc("GET /api/versions/download", handleVersionDownload)
	mux.HandleFunc("POST /api/versions/restore", handleVersionRestore)
	mux.HandleFunc("/", handleStatic)

	var h http.Handler = mux
	if gate != nil {
		h = gate.Require(h)
	}
	return web.Guard(h, hosts)
}

// healthcheck probes the local server for the container HEALTHCHECK, since
// the distroless image has no wget or curl. Over HTTPS it trusts exactly
// Strata's own certificate (pinned from disk) rather than skipping checks.
func healthcheck(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	target := net.JoinHostPort(host, port)

	pool, name, leaf := pinnedCert()
	if pool != nil {
		c := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: pool, ServerName: name, MinVersion: tls.VersionTLS12,
				// Liveness, not certificate hygiene: an expired certificate
				// still identifies our own server, so check at a valid moment.
				Time: func() time.Time { return validAt(leaf, time.Now()) },
			},
		}}
		if probe(c, "https://"+target) {
			return 0
		}
	}
	if probe(http.Client{Timeout: 3 * time.Second}, "http://"+target) {
		return 0
	}
	return 1
}

func probe(c http.Client, base string) bool {
	res, err := c.Get(base + "/api/health")
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// validAt returns now, or a moment inside the leaf's validity if now isn't.
func validAt(leaf *x509.Certificate, now time.Time) time.Time {
	if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return leaf.NotBefore.Add(leaf.NotAfter.Sub(leaf.NotBefore) / 2)
	}
	return now
}

// pinnedCert loads the certificate the server is serving, from the same
// settings the server reads, and picks a name it is valid for.
func pinnedCert() (*x509.CertPool, string, *x509.Certificate) {
	files := []string{os.Getenv("STRATA_TLS_CERT")}
	if files[0] == "" {
		files = []string{
			filepath.Join(envOr("STRATA_CONFIG", "/config"), "tls", "cert.pem"),
			filepath.Join(tlsutil.ScratchDir(), "tls", "cert.pem"),
		}
	}
	var raw []byte
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil {
			raw = b
			break
		}
	}
	if raw == nil {
		return nil, "", nil
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, "", nil
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, "", nil
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(raw)
	pool.AddCert(leaf)
	switch {
	case slices.Contains(leaf.DNSNames, "localhost"):
		return pool, "localhost", leaf
	case len(leaf.DNSNames) > 0:
		return pool, leaf.DNSNames[0], leaf
	case len(leaf.IPAddresses) > 0:
		return pool, leaf.IPAddresses[0].String(), leaf
	}
	return nil, "", nil
}

func envInt(key string, fallback int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── handlers ───────────────────────────────────────────────────────────────

func handleList(w http.ResponseWriter, r *http.Request) {
	listing, err := fileops.List(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, listing)
}

func handleUsage(w http.ResponseWriter, _ *http.Request) {
	u, err := fileops.Usage(root)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, u)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, r.URL.Query().Get("path"), true)
}

func handleRaw(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, r.URL.Query().Get("path"), false)
}

// serveFile streams a file, honouring Range requests so video scrubbing and
// PDF page jumps work without downloading the whole file.
func serveFile(w http.ResponseWriter, r *http.Request, rel string, attach bool) {
	rel, err := safepath.User(rel)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := root.FS().Open(rel)
	if err != nil {
		fail(w, safepath.Err(err))
		return
	}
	defer f.Close()

	serveOpen(w, r, f, path.Base(rel), attach)
}

// serveOpen streams an open file with the hardened raw-file headers.
func serveOpen(w http.ResponseWriter, r *http.Request, f *os.File, name string, attach bool) {
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", web.RawHeaders(w, filepath.Ext(name), mime.TypeByExtension(filepath.Ext(name))))
	disp := "inline"
	if attach {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", contentDisposition(disp, name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// contentDisposition carries any filename: an ASCII fallback for old
// clients plus the exact UTF-8 name per RFC 6266 / 5987.
func contentDisposition(disp, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return disp + `; filename="` + ascii + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// handleText returns a text file's contents, capped so a stray 2 GB log does
// not take the browser down with it.
func handleText(w http.ResponseWriter, r *http.Request) {
	t, err := fileops.ReadText(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, t)
}

func handleTextWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Version string `json:"version"`
		Content string `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	v, err := fileops.WriteText(root, req.Path, req.Version, req.Content)
	if err != nil {
		fail(w, err)
		return
	}
	appState.Touch(safepath.Clean(req.Path))
	writeJSON(w, map[string]any{"version": v})
}

func handleThumb(w http.ResponseWriter, r *http.Request) {
	if thumbs == nil {
		http.Error(w, "no thumbnails", http.StatusNotFound)
		return
	}
	b, ct, err := thumbs.Get(r.Context(), root, r.URL.Query().Get("path"))
	if errors.Is(err, thumb.ErrUnsupported) {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	// The URL carries the mtime, so a changed file gets a new URL.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Write(b)
}

// handleState returns favorites and recent files that still exist.
func handleState(w http.ResponseWriter, _ *http.Request) {
	favs, recent := appState.Snapshot()
	type recentEntry struct {
		fileops.Entry
		Opened time.Time `json:"opened"`
	}
	out := struct {
		Enabled   bool            `json:"enabled"`
		Favorites []fileops.Entry `json:"favorites"`
		Recent    []recentEntry   `json:"recent"`
	}{appState.Enabled(), []fileops.Entry{}, []recentEntry{}}
	for _, p := range favs {
		if e, err := fileops.Stat(root, p); err == nil {
			out.Favorites = append(out.Favorites, e)
		}
	}
	for _, rc := range recent {
		if e, err := fileops.Stat(root, rc.Path); err == nil && !e.IsDir {
			out.Recent = append(out.Recent, recentEntry{e, rc.Time})
		}
	}
	writeJSON(w, out)
}

func handleFavorite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		On   bool   `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, err := fileops.Stat(root, req.Path)
	if err != nil && req.On {
		fail(w, err)
		return
	}
	p := safepath.Clean(req.Path)
	if err == nil {
		p = e.Path
	}
	if err := appState.SetFavorite(p, req.On); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleRecent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	e, err := fileops.Stat(root, req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	if !e.IsDir {
		appState.Touch(e.Path)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleSheetRead(w http.ResponseWriter, r *http.Request) {
	grid, err := sheet.Read(root, r.URL.Query().Get("path"), r.URL.Query().Get("sheet"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, grid)
}

func handleSheetWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string       `json:"path"`
		Sheet   string       `json:"sheet"`
		Version string       `json:"version"`
		Edits   []sheet.Edit `json:"edits"`
	}
	if !decode(w, r, &req) {
		return
	}
	grid, err := sheet.Write(root, req.Path, req.Sheet, req.Version, req.Edits)
	if err != nil {
		fail(w, err)
		return
	}
	appState.Touch(safepath.Clean(req.Path))
	writeJSON(w, grid)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")

	// Cap the whole request; exceeding it fails the write instead of
	// silently truncating the file.
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body", http.StatusBadRequest)
		return
	}

	saved := []string{}
	relPath := "" // from a "relpath" field sent just before its file
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fail(w, err)
			return
		}
		if part.FormName() == "relpath" {
			b, _ := io.ReadAll(io.LimitReader(part, 4<<10))
			part.Close()
			relPath = string(b)
			continue
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}

		name := relPath
		relPath = ""
		if name == "" {
			name = part.FileName() // already reduced to a base name
		}
		rel, err := uploadName(name)
		if err != nil {
			part.Close()
			fail(w, err)
			return
		}
		dest := path.Join(dir, rel)

		err = fileops.Save(root, dest, part)
		part.Close()
		if err != nil {
			fail(w, err)
			return
		}
		saved = append(saved, dest)
	}

	writeJSON(w, map[string]any{"saved": saved})
}

// uploadName vets a client-supplied relative path for a folder upload:
// every component must be a plain name, so nothing climbs out of the
// target folder or lands in Strata's internal directory.
func uploadName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) > 64 {
		return "", userr.Error("folder nesting is too deep")
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || p == safepath.Internal || strings.ContainsRune(p, 0) {
			return "", userr.Error("invalid file name")
		}
	}
	return strings.Join(parts, "/"), nil
}

func handleUploadStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir  string `json:"dir"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if !decode(w, r, &req) {
		return
	}
	rel, err := uploadName(req.Name)
	if err != nil {
		fail(w, err)
		return
	}
	id, err := fileops.StartUpload(root, path.Join(safepath.Clean(req.Dir), rel), req.Size, maxUpload)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"id": id, "chunk": fileops.ChunkMax / 2})
}

func handleUploadOffset(w http.ResponseWriter, r *http.Request) {
	off, size, err := fileops.UploadOffset(root, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(off, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "no-store")
}

func handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	off, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || off < 0 {
		http.Error(w, "missing Upload-Offset", http.StatusBadRequest)
		return
	}
	body := http.MaxBytesReader(w, r.Body, fileops.ChunkMax)
	next, err := fileops.WriteChunk(root, r.PathValue("id"), off, body)
	w.Header().Set("Upload-Offset", strconv.FormatInt(next, 10))
	var oe *fileops.OffsetError
	if errors.As(err, &oe) {
		http.Error(w, "offset mismatch", http.StatusConflict)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleUploadFinish(w http.ResponseWriter, r *http.Request) {
	p, err := fileops.FinishUpload(root, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"saved": []string{p}})
}

func handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	if err := fileops.AbortUpload(root, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	res, err := fileops.Search(ctx, root, r.URL.Query().Get("path"), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, res)
}

// handleZip streams a selection as one archive. It is a GET so the browser
// downloads it natively; reading needs no CSRF token, only a session.
func handleZip(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	paths, err := fileops.ZipCheck(root, q["path"])
	if err != nil {
		fail(w, err)
		return
	}
	name := "strata.zip"
	if len(paths) == 1 && paths[0] != "." {
		name = path.Base(paths[0]) + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	w.Header().Set("Cache-Control", "no-store")
	if err := fileops.Zip(r.Context(), root, paths, w); err != nil {
		// Headers are gone; abort the connection so the client sees a
		// failed download instead of a truncated archive.
		log.Printf("zip: %v", err)
		panic(http.ErrAbortHandler)
	}
}

func handleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := fileops.Mkdir(root, req.Path); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := fileops.Move(root, req.From, req.To); err != nil {
		fail(w, err)
		return
	}
	appState.Rename(safepath.Clean(req.From), safepath.Clean(req.To))
	writeJSON(w, map[string]any{"ok": true})
}

func handleCopy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := fileops.Copy(root, req.From, req.To); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleDelete moves to the trash unless the caller asks for a permanent
// delete (the UI only offers that when a location has no trash).
func handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Permanent bool   `json:"permanent"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Permanent {
		if err := fileops.Delete(root, req.Path); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	id, err := fileops.Trash(root, req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "trashId": id})
}

func handleTrashList(w http.ResponseWriter, _ *http.Request) {
	items, err := fileops.ListTrash(root)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

func handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := fileops.Restore(root, req.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"path": p})
}

func handleTrashPurge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  string `json:"id"`
		All bool   `json:"all"`
	}
	if !decode(w, r, &req) {
		return
	}
	var err error
	if req.All {
		err = fileops.EmptyTrash(root)
	} else {
		err = fileops.Purge(root, req.ID)
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := fileops.ListVersions(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"versions": vs})
}

func handleVersionDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f, err := fileops.OpenVersion(root, q.Get("path"), q.Get("id"))
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()
	serveOpen(w, r, f, path.Base(safepath.Clean(q.Get("path"))), true)
}

func handleVersionRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := fileops.RestoreVersion(root, req.Path, req.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// sweep enforces trash, version and partial-upload retention.
func sweep(trashDays int) {
	for {
		fileops.SweepUploads(root, fileops.UploadMaxAge)
		if n, _ := fileops.SweepTrash(root, time.Duration(trashDays)*24*time.Hour); n > 0 {
			log.Printf("trash: purged %d item(s) older than %d days", n, trashDays)
		}
		fileops.SweepVersions(root)
		time.Sleep(24 * time.Hour)
	}
}

// handleStatic serves the embedded SPA, falling back to index.html so client
// routing works on a hard refresh.
func handleStatic(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		http.Error(w, "ui not built", http.StatusInternalServerError)
		return
	}

	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}

	if f, err := sub.Open(p); err == nil {
		defer f.Close()
		if st, err := f.Stat(); err == nil && !st.IsDir() {
			if ct := mime.TypeByExtension(filepath.Ext(p)); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			// Hashed asset filenames are safe to cache hard; index.html is not.
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if rs, ok := f.(io.ReadSeeker); ok {
				http.ServeContent(w, r, p, st.ModTime(), rs)
			} else {
				io.Copy(w, f)
			}
			return
		}
	}

	index, err := sub.Open("index.html")
	if err != nil {
		http.Error(w, "ui not built", http.StatusInternalServerError)
		return
	}
	defer index.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.Copy(w, index)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	// A JSON-only API can't be hit by a cross-site form or text/plain fetch.
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(v); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

// fail maps an internal error to a status code without leaking host paths
// back to the client.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, safepath.ErrEscape):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, os.ErrPermission):
		log.Printf("error: %v", err)
		http.Error(w, "permission denied", http.StatusForbidden)
	case errors.Is(err, fileops.ErrNoTrash):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, sheet.ErrConflict), errors.Is(err, fileops.ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.As(err, new(*http.MaxBytesError)):
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
	case errors.As(err, new(userr.Error)):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		// OS errors carry host paths; keep them in the log only.
		log.Printf("error: %v", err)
		http.Error(w, "request failed", http.StatusBadRequest)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/assets/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
