// Strata — a small, self-hosted file manager for a TrueNAS share.
//
// Access control is deliberately absent: this is designed to be published
// only on a Tailscale tailnet, where the network itself is the boundary.
// Do not expose it to the public internet.
package main

import (
	"bufio"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"strata/internal/auth"
	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/sheet"
	"strata/internal/tlsutil"
	"strata/internal/userr"
	"strata/internal/web"
)

//go:embed all:dist
var embedded embed.FS

// maxUpload caps a single uploaded file. NAS users move large media, so this
// is generous; the reverse proxy should carry a matching limit.
const maxUpload = 8 << 30 // 8 GiB

var root *safepath.Root

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck(envOr("STRATA_ADDR", ":8080")))
		case "hash-password":
			os.Exit(hashPassword())
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
	)
	flag.Parse()

	r, err := safepath.NewRoot(*dir)
	if err != nil {
		log.Fatalf("root %q unusable: %v", *dir, err)
	}
	root = r
	log.Printf("serving %s on %s", root.Path(), *addr)

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
	mux.HandleFunc("GET /api/sheet", handleSheetRead)
	mux.HandleFunc("POST /api/sheet", handleSheetWrite)
	mux.HandleFunc("POST /api/upload", handleUpload)
	mux.HandleFunc("POST /api/mkdir", handleMkdir)
	mux.HandleFunc("POST /api/move", handleMove)
	mux.HandleFunc("POST /api/copy", handleCopy)
	mux.HandleFunc("POST /api/delete", handleDelete)
	mux.HandleFunc("/", handleStatic)

	var h http.Handler = mux
	if gate != nil {
		h = gate.Require(h)
	}
	return web.Guard(h, hosts)
}

// healthcheck probes the local server for the container HEALTHCHECK, since
// the distroless image has no wget or curl.
func healthcheck(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	// The probe can't know which TLS mode the server chose, so try HTTPS
	// first. Skipping verification is fine: it only ever talks to loopback.
	c := http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- loopback probe
		},
	}
	for _, scheme := range []string{"https", "http"} {
		res, err := c.Get(scheme + "://" + net.JoinHostPort(host, port) + "/api/health")
		if err != nil {
			continue
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			return 0
		}
	}
	return 1
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

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}

	name := path.Base(rel)
	w.Header().Set("Content-Type", web.RawHeaders(w, filepath.Ext(name), mime.TypeByExtension(filepath.Ext(name))))
	disp := "inline"
	if attach {
		disp = "attachment"
	}
	// Quote the filename so spaces and punctuation survive the header.
	w.Header().Set("Content-Disposition", disp+`; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)

	http.ServeContent(w, r, name, info.ModTime(), f)
}

// handleText returns a text file's contents, capped so a stray 2 GB log does
// not take the browser down with it.
func handleText(w http.ResponseWriter, r *http.Request) {
	const limit = 2 << 20 // 2 MiB

	rel, err := safepath.User(r.URL.Query().Get("path"))
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

	buf, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		fail(w, err)
		return
	}
	truncated := len(buf) > limit
	if truncated {
		buf = buf[:limit]
	}
	writeJSON(w, map[string]any{
		"content":   string(buf),
		"truncated": truncated,
	})
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
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fail(w, err)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}

		// Trust only the base name; browsers may send a full path for
		// directory uploads and we do not want to honour it blindly.
		name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "." || name == ".." || name == "/" {
			part.Close()
			fail(w, userr.Error("invalid file name"))
			return
		}
		dest := path.Join(dir, name)

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

func handleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := fileops.Delete(root, req.Path); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
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
	case errors.Is(err, sheet.ErrConflict):
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
