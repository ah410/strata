package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strata/internal/auth"
	"strata/internal/safepath"
)

const testPass = "correct horse battery"

func authServer(t *testing.T, proxies ...string) http.Handler {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	r, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	root = r

	h, err := auth.Hash(testPass)
	if err != nil {
		t.Fatal(err)
	}
	pw := filepath.Join(t.TempDir(), "pw")
	os.WriteFile(pw, []byte(h+"\n"), 0o600)
	gate, err := loadGate("passphrase", pw, proxies)
	if err != nil {
		t.Fatal(err)
	}
	return newHandler("Strata", []string{"strata.example.com"}, gate)
}

type call struct {
	method, path, body, remote string
	hdr                        map[string]string
	cookie                     *http.Cookie
}

func (c call) do(h http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	req.Host = "strata.example.com"
	req.RemoteAddr = "127.0.0.1:5555"
	if c.remote != "" {
		req.RemoteAddr = c.remote
	}
	for k, v := range c.hdr {
		req.Header.Set(k, v)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var jsonPost = map[string]string{"Content-Type": "application/json", "X-Strata": "1"}

func login(h http.Handler, pass string, remote string, extra map[string]string) *httptest.ResponseRecorder {
	hdr := map[string]string{}
	for k, v := range jsonPost {
		hdr[k] = v
	}
	for k, v := range extra {
		hdr[k] = v
	}
	return call{method: "POST", path: "/api/login", body: `{"passphrase":"` + pass + `"}`, remote: remote, hdr: hdr}.do(h)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", rec.Header()["Set-Cookie"])
	return nil
}

func TestAPIRequiresSession(t *testing.T) {
	h := authServer(t)
	for _, p := range []string{"/api/list?path=", "/api/raw?path=a.txt", "/api/download?path=a.txt", "/api/text?path=a.txt", "/api/usage", "/api/session"} {
		if got := (call{method: "GET", path: p}).do(h).Code; got != http.StatusUnauthorized {
			t.Errorf("GET %s without session: %d, want 401", p, got)
		}
	}
	post := call{method: "POST", path: "/api/delete", body: `{"path":"a.txt"}`, hdr: jsonPost}
	if got := post.do(h).Code; got != http.StatusUnauthorized {
		t.Errorf("delete without session: %d", got)
	}
	if got := (call{method: "GET", path: "/api/health"}).do(h).Code; got != 200 {
		t.Errorf("health: %d", got)
	}
	if got := (call{method: "GET", path: "/"}).do(h).Code; got != 200 {
		t.Errorf("UI shell: %d", got)
	}
}

func TestLoginFlow(t *testing.T) {
	h := authServer(t)
	if rec := login(h, "wrong passphrase!", "", nil); rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("wrong passphrase: %d cookies=%v", rec.Code, rec.Result().Cookies())
	}

	rec := login(h, testPass, "", nil)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	set := rec.Header().Get("Set-Cookie")
	for _, attr := range []string{"__Host-strata=", "Path=/", "HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(set, attr) {
			t.Errorf("cookie missing %q: %s", attr, set)
		}
	}
	if strings.Contains(strings.ToLower(set), "domain=") {
		t.Errorf("cookie has a Domain: %s", set)
	}
	c := sessionCookie(t, rec)

	if got := (call{method: "GET", path: "/api/list?path=", cookie: c}).do(h).Code; got != 200 {
		t.Fatalf("list with session: %d", got)
	}
	if got := (call{method: "POST", path: "/api/logout", hdr: jsonPost, cookie: c}).do(h).Code; got != 200 {
		t.Fatalf("logout: %d", got)
	}
	if got := (call{method: "GET", path: "/api/list?path=", cookie: c}).do(h).Code; got != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", got)
	}
}

func TestLoginGuards(t *testing.T) {
	h := authServer(t, "172.18.0.0/16")

	// Login CSRF: a cross-site form can't sign the victim in.
	rec := call{method: "POST", path: "/api/login", body: `{"passphrase":"` + testPass + `"}`,
		hdr: map[string]string{"Content-Type": "application/json"}}.do(h)
	if rec.Code != http.StatusForbidden {
		t.Errorf("login without X-Strata: %d", rec.Code)
	}

	// Plain HTTP from the LAN would put the cookie on the wire; refuse.
	if rec := login(h, testPass, "192.168.0.50:4000", nil); rec.Code != http.StatusForbidden {
		t.Errorf("plain-HTTP LAN login: %d", rec.Code)
	}
	// Only a trusted proxy can vouch for HTTPS.
	if rec := login(h, testPass, "192.168.0.50:4000", map[string]string{"X-Forwarded-Proto": "https"}); rec.Code != http.StatusForbidden {
		t.Errorf("spoofed X-Forwarded-Proto: %d", rec.Code)
	}
	if rec := login(h, testPass, "172.18.0.9:4000", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-For": "192.168.0.50"}); rec.Code != 200 {
		t.Errorf("login via trusted HTTPS proxy: %d %s", rec.Code, rec.Body)
	}
}

func TestBruteForceIsThrottled(t *testing.T) {
	h := authServer(t)
	for i := 1; i <= 5; i++ {
		if got := login(h, "wrong passphrase!", "", nil).Code; got != http.StatusUnauthorized {
			t.Fatalf("wrong attempt %d: %d, want 401", i, got)
		}
	}
	// Locked out now, even with the right passphrase.
	rec := login(h, testPass, "", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked-out client got %d (Retry-After %q)", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestFailClosedWithoutPassphrase(t *testing.T) {
	if _, err := loadGate("passphrase", "", nil); err == nil {
		t.Error("started with no passphrase configured")
	}
	if _, err := loadGate("passphrase", "/nonexistent", nil); err == nil {
		t.Error("started with a missing password file")
	}
	bad := filepath.Join(t.TempDir(), "pw")
	os.WriteFile(bad, []byte("hunter2"), 0o600)
	if _, err := loadGate("passphrase", bad, nil); err == nil {
		t.Error("started with a plaintext password file")
	}
	if _, err := loadGate("off", "", nil); err == nil {
		t.Error("unknown auth mode accepted")
	}
	if g, err := loadGate("none", "", nil); err != nil || g != nil {
		t.Error("explicit auth=none should disable the gate")
	}
}
