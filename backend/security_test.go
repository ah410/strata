package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strata/internal/safepath"
)

func testServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = r
	return newHandler("Strata", []string{"strata.example.com"}, nil), dir
}

func do(h http.Handler, method, target, host, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHostAllowlist(t *testing.T) {
	h, _ := testServer(t)
	cases := map[string]int{
		"192.168.0.78:8081":      200,
		"[::1]:8080":             200,
		"localhost:8080":         200,
		"strata.example.com":     200,
		"STRATA.example.com:443": 200,
		"evil.example":           http.StatusMisdirectedRequest,
		"rebind.attacker.test":   http.StatusMisdirectedRequest,
		"":                       http.StatusMisdirectedRequest,
	}
	for host, want := range cases {
		if got := do(h, "GET", "/api/list?path=", host, "", nil).Code; got != want {
			t.Errorf("host %q: got %d, want %d", host, got, want)
		}
	}
}

func TestStateChangeRequiresSameOrigin(t *testing.T) {
	h, dir := testServer(t)
	json := map[string]string{"Content-Type": "application/json"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{"Content-Type": "application/json", "X-Strata": "1"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	body := `{"path":"made"}`

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"no guard header", json, http.StatusForbidden},
		{"cross-site fetch metadata", with(map[string]string{"Sec-Fetch-Site": "cross-site"}), http.StatusForbidden},
		{"same-site is not same-origin", with(map[string]string{"Sec-Fetch-Site": "same-site"}), http.StatusForbidden},
		{"foreign origin", with(map[string]string{"Origin": "https://evil.example"}), http.StatusForbidden},
		{"text/plain simple request", map[string]string{"Content-Type": "text/plain", "X-Strata": "1"}, http.StatusUnsupportedMediaType},
		{"same origin", with(map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://strata.example.com"}), 200},
	}
	for _, c := range cases {
		if got := do(h, "POST", "/api/mkdir", "strata.example.com", body, c.hdr).Code; got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
		if c.want != 200 {
			if _, err := os.Stat(filepath.Join(dir, "made")); err == nil {
				t.Fatalf("%s: directory was created despite rejection", c.name)
			}
		}
	}
}

func TestRawNeverExecutesUserFiles(t *testing.T) {
	h, dir := testServer(t)
	files := map[string]string{
		"x.html": "<script>alert(1)</script>",
		"x.svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"x.js":   "alert(1)",
		"x.png":  "\x89PNG",
		"x.pdf":  "%PDF-1.4",
	}
	for name, body := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	want := map[string]struct {
		ct      string
		sandbox bool
	}{
		"x.html": {"text/plain; charset=utf-8", true},
		"x.svg":  {"image/svg+xml", true},
		"x.js":   {"text/plain; charset=utf-8", true},
		"x.png":  {"image/png", true},
		"x.pdf":  {"application/pdf", false},
	}
	for name, w := range want {
		rec := do(h, "GET", "/api/raw?path="+name, "strata.example.com", "", nil)
		if ct := rec.Header().Get("Content-Type"); ct != w.ct {
			t.Errorf("%s: Content-Type %q, want %q", name, ct, w.ct)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if strings.Contains(csp, "sandbox") != w.sandbox {
			t.Errorf("%s: CSP %q, sandbox want %v", name, csp, w.sandbox)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing nosniff", name)
		}
	}
}

func TestErrorsDoNotLeakHostPaths(t *testing.T) {
	h, dir := testServer(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	// Listing a file (not a directory) is a user error with a safe message.
	rec := do(h, "GET", "/api/list?path=a.txt", "strata.example.com", "", nil)
	if rec.Code != 400 || strings.TrimSpace(rec.Body.String()) != "not a directory" {
		t.Errorf("user error: got %d %q", rec.Code, rec.Body.String())
	}
	// Opening a non-workbook surfaces an internal error; its text must not.
	rec = do(h, "GET", "/api/sheet?path=a.txt", "strata.example.com", "", nil)
	if strings.Contains(rec.Body.String(), dir) {
		t.Errorf("response leaks host path: %q", rec.Body.String())
	}
}

func TestSecurityHeadersOnUI(t *testing.T) {
	h, _ := testServer(t)
	rec := do(h, "GET", "/", "strata.example.com", "", nil)
	csp := rec.Header().Get("Content-Security-Policy")
	for _, d := range []string{"script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP missing %q: %q", d, csp)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Error("inline scripts allowed")
	}
}
