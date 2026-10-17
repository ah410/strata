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
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/sheet"
)

//go:embed all:dist
var embedded embed.FS

// maxUpload caps a single uploaded file. NAS users move large media, so this
// is generous; the reverse proxy should carry a matching limit.
const maxUpload = 8 << 30 // 8 GiB

var root *safepath.Root

func main() {
	var (
		dir  = flag.String("root", envOr("STRATA_ROOT", "/data"), "directory to serve")
		addr = flag.String("addr", envOr("STRATA_ADDR", ":8080"), "listen address")
		name = flag.String("name", envOr("STRATA_NAME", "Strata"), "display name")
	)
	flag.Parse()

	r, err := safepath.NewRoot(*dir)
	if err != nil {
		log.Fatalf("root %q unusable: %v", *dir, err)
	}
	root = r
	log.Printf("serving %s on %s", root.Path(), *addr)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "name": *name})
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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: large downloads and uploads legitimately run long.
	}
	log.Fatal(srv.ListenAndServe())
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
	abs, err := root.Resolve(rel)
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}

	name := filepath.Base(abs)
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
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

	abs, err := root.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		fail(w, err)
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
	abs, err := root.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	grid, err := sheet.Read(abs, r.URL.Query().Get("sheet"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, grid)
}

func handleSheetWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string       `json:"path"`
		Sheet string       `json:"sheet"`
		Edits []sheet.Edit `json:"edits"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	abs, err := root.Resolve(req.Path)
	if err != nil {
		fail(w, err)
		return
	}
	grid, err := sheet.Write(abs, req.Sheet, req.Edits)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, grid)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")

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
		name := filepath.Base(part.FileName())
		dest := path.Join(dir, name)

		err = fileops.Save(root, dest, io.LimitReader(part, maxUpload))
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
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
	default:
		log.Printf("error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
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
