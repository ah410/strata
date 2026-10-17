// Strata — a small, self-hosted file manager for a TrueNAS share.
//
// Access control is deliberately absent: this is designed to be published
// only on a Tailscale tailnet, where the network itself is the boundary.
// Do not expose it to the public internet.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
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

	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/sheet"
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
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(envOr("STRATA_ADDR", ":8080")))
	}

	var (
		dir  = flag.String("root", envOr("STRATA_ROOT", "/data"), "directory to serve")
		addr = flag.String("addr", envOr("STRATA_ADDR", ":8080"), "listen address")
		name = flag.String("name", envOr("STRATA_NAME", "Strata"), "display name")
		// Hostnames (no port) the UI is reached by, e.g. strata.example.com.
		// IP addresses and localhost always work; anything else is refused.
		hosts = flag.String("allowed-hosts", envOr("STRATA_ALLOWED_HOSTS", ""), "comma-separated hostnames")
	)
	flag.Parse()

	r, err := safepath.NewRoot(*dir)
	if err != nil {
		log.Fatalf("root %q unusable: %v", *dir, err)
	}
	root = r
	log.Printf("serving %s on %s", root.Path(), *addr)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(newHandler(*name, strings.Split(*hosts, ","))),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		// No write timeout: large downloads and uploads legitimately run long.
	}
	log.Fatal(srv.ListenAndServe())
}

// newHandler builds the routes behind the request guards.
func newHandler(name string, hosts []string) http.Handler {
	mux := http.NewServeMux()

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

	return web.Guard(mux, hosts)
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
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + net.JoinHostPort(host, port) + "/api/health")
	if err != nil {
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
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
	f, err := root.FS().Open(safepath.Clean(rel))
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

	name := path.Base(safepath.Clean(rel))
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

	f, err := root.FS().Open(safepath.Clean(r.URL.Query().Get("path")))
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
