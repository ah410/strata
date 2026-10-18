package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupReq(g *Gate, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/setup", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.Setup(w, r)
	return w
}

func TestFirstRunSetup(t *testing.T) {
	hashPath := filepath.Join(t.TempDir(), "auth.hash")
	g, code, err := NewSetupGate(hashPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalizeCode(code)) != 20 {
		t.Fatalf("code %q is not 20 characters", code)
	}

	// Everything protected says "setup required" until it's done.
	protected := httptest.NewRecorder()
	g.Require(http.NotFoundHandler()).ServeHTTP(protected, httptest.NewRequest("GET", "/api/list", nil))
	if protected.Code != http.StatusPreconditionRequired {
		t.Fatalf("protected route before setup: %d", protected.Code)
	}

	if w := setupReq(g, `{"code":"WRONG-CODE","passphrase":"correct horse battery"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong code: %d", w.Code)
	}
	if _, err := os.Stat(hashPath); err == nil {
		t.Fatal("hash written despite wrong code")
	}
	if w := setupReq(g, `{"code":"`+code+`","passphrase":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short passphrase: %d", w.Code)
	}

	// Codes are accepted however they're typed.
	lower := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	w := setupReq(g, `{"code":"`+lower+`","passphrase":"correct horse battery"}`)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Set-Cookie"), CookieName) {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	info, err := os.Stat(hashPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("hash file mode %v, want owner-only", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(hashPath)
	v, err := NewVerifier(string(raw))
	if err != nil || !v.Verify("correct horse battery") {
		t.Fatal("stored hash does not verify")
	}

	// Setup is single-use: the same code can't reset the passphrase.
	if w := setupReq(g, `{"code":"`+code+`","passphrase":"attacker passphrase!"}`); w.Code != http.StatusConflict {
		t.Errorf("second setup: %d", w.Code)
	}
}

func TestSetupIsThrottledAndNeedsHTTPS(t *testing.T) {
	g, code, _ := NewSetupGate(filepath.Join(t.TempDir(), "auth.hash"), nil)
	for i := 0; i < freeFailures; i++ {
		setupReq(g, `{"code":"AAAA-AAAA-AAAA-AAAA-AAAA","passphrase":"correct horse battery"}`)
	}
	if w := setupReq(g, `{"code":"`+code+`","passphrase":"correct horse battery"}`); w.Code != http.StatusTooManyRequests {
		t.Errorf("guessing not throttled: %d", w.Code)
	}

	g2, code2, _ := NewSetupGate(filepath.Join(t.TempDir(), "auth.hash"), nil)
	r := httptest.NewRequest("POST", "/api/setup", strings.NewReader(`{"code":"`+code2+`","passphrase":"correct horse battery"}`))
	r.RemoteAddr = "192.168.0.50:4000"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g2.Setup(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("plain-HTTP LAN setup: %d", w.Code)
	}
}

func TestCodesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		_, c, _ := NewSetupGate(filepath.Join(t.TempDir(), "h"), nil)
		if seen[c] {
			t.Fatal("duplicate setup code")
		}
		seen[c] = true
	}
}
