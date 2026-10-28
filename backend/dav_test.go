package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strata/internal/auth"
	"strata/internal/fileops"
)

const davPass = "correct horse battery"

func davServer(t *testing.T, on bool) (http.Handler, string) {
	t.Helper()
	_, dir := testServer(t)
	hash, err := auth.Hash(davPass)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := auth.NewVerifier(hash)
	gate, _ := auth.NewGate(v, nil)
	davEnabled = on
	t.Cleanup(func() { davEnabled = false })
	return newHandler("Strata", []string{"strata.example.com"}, gate), dir
}

func davDo(h http.Handler, method, target, pass string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	req.Host = "strata.example.com"
	req.RemoteAddr = "127.0.0.1:5555" // loopback counts as secure
	if pass != "" {
		req.SetBasicAuth("me", pass)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDavNeedsThePassphrase(t *testing.T) {
	h, _ := davServer(t, true)
	rec := davDo(h, "PROPFIND", "/dav/", "", nil, nil)
	if rec.Code != 401 || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("no creds: %d %v", rec.Code, rec.Header())
	}
	if rec := davDo(h, "PROPFIND", "/dav/", "wrong", nil, nil); rec.Code != 401 {
		t.Fatalf("wrong pass: %d", rec.Code)
	}
	if rec := davDo(h, "PROPFIND", "/dav/", davPass, nil, map[string]string{"Depth": "1"}); rec.Code != 207 {
		t.Fatalf("right pass: %d %s", rec.Code, rec.Body)
	}
}

func TestDavKeepsTheSafetyNet(t *testing.T) {
	h, dir := davServer(t, true)
	os.MkdirAll(filepath.Join(dir, ".strata/trash"), 0o755)

	if rec := davDo(h, "PUT", "/dav/notes.txt", davPass, strings.NewReader("v1"), nil); rec.Code != 201 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	davDo(h, "PUT", "/dav/notes.txt", davPass, strings.NewReader("v2"), nil)
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "v2" {
		t.Fatalf("content = %q", b)
	}
	if vs, _ := fileops.ListVersions(root, "notes.txt"); len(vs) != 1 {
		t.Fatalf("overwrite kept %d versions, want 1", len(vs))
	}

	rec := davDo(h, "PROPFIND", "/dav/", davPass, nil, map[string]string{"Depth": "1"})
	if !strings.Contains(rec.Body.String(), "notes.txt") || strings.Contains(rec.Body.String(), ".strata") {
		t.Fatalf("listing: %s", rec.Body)
	}

	rec = davDo(h, "GET", "/dav/notes.txt", davPass, nil, nil)
	if rec.Body.String() != "v2" || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") || rec.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("get: %q %v", rec.Body, rec.Header())
	}

	if rec := davDo(h, "MOVE", "/dav/notes.txt", davPass, nil, map[string]string{"Destination": "http://strata.example.com/dav/renamed.txt"}); rec.Code != 201 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body)
	}
	if rec := davDo(h, "DELETE", "/dav/renamed.txt", davPass, nil, nil); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if items, _ := fileops.ListTrash(root); len(items) != 1 || items[0].Path != "renamed.txt" {
		t.Fatalf("delete didn't go to the trash: %+v", items)
	}
}

func TestDavHidesInternalFiles(t *testing.T) {
	h, dir := davServer(t, true)
	os.MkdirAll(filepath.Join(dir, ".strata/trash/x"), 0o755)
	os.WriteFile(filepath.Join(dir, ".strata/trash/x/item"), []byte("secret"), 0o644)
	os.WriteFile(filepath.Join(dir, ".strata-tmp"), []byte("tmp"), 0o644)
	for _, p := range []string{"/dav/.strata/trash/x/item", "/dav/.strata-tmp", "/dav/../etc/passwd"} {
		if rec := davDo(h, "GET", p, davPass, nil, nil); rec.Code == 200 {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	if rec := davDo(h, "PUT", "/dav/.strata/evil", davPass, strings.NewReader("x"), nil); rec.Code < 400 {
		t.Errorf("PUT into .strata: %d", rec.Code)
	}
	if rec := davDo(h, "DELETE", "/dav/.strata", davPass, nil, nil); rec.Code < 400 {
		t.Errorf("DELETE .strata: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".strata/trash/x/item")); err != nil {
		t.Fatal("internal file touched")
	}
}

func TestDavRefusesWebPages(t *testing.T) {
	h, dir := davServer(t, true)
	if rec := davDo(h, "POST", "/dav/x", davPass, strings.NewReader("x"), nil); rec.Code < 400 {
		t.Errorf("POST: %d", rec.Code)
	}
	hdr := map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}
	if rec := davDo(h, "PUT", "/dav/x", davPass, strings.NewReader("x"), hdr); rec.Code != 403 {
		t.Errorf("cross-site PUT: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("forged request wrote a file")
	}
}

type brokenBody struct{ sent bool }

func (b *brokenBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, errors.New("connection reset")
	}
	b.sent = true
	return copy(p, "half a fi"), nil
}

func TestDavInterruptedPutLeavesFileAlone(t *testing.T) {
	h, dir := davServer(t, true)
	os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("original"), 0o644)
	davDo(h, "PUT", "/dav/doc.txt", davPass, &brokenBody{}, nil)
	if b, _ := os.ReadFile(filepath.Join(dir, "doc.txt")); string(b) != "original" {
		t.Fatalf("interrupted PUT replaced the file with %q", b)
	}
}

func TestDavOffByDefault(t *testing.T) {
	h, dir := davServer(t, false)
	davDo(h, "PUT", "/dav/x", davPass, strings.NewReader("x"), nil)
	if rec := davDo(h, "PROPFIND", "/dav/", davPass, nil, nil); rec.Code == 207 {
		t.Fatal("WebDAV answered while off")
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("PUT wrote with WebDAV off")
	}
}
