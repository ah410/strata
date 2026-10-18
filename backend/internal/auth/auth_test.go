package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func TestHashVerify(t *testing.T) {
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Errorf("unexpected hash format %q", h)
	}
	v, err := NewVerifier(h + "\n") // files usually end in a newline
	if err != nil {
		t.Fatal(err)
	}
	if !v.Verify("correct horse battery") {
		t.Error("right passphrase rejected")
	}
	for _, wrong := range []string{"", "correct horse batter", "Correct horse battery", "correct horse battery "} {
		if v.Verify(wrong) {
			t.Errorf("wrong passphrase %q accepted", wrong)
		}
	}
	h2, _ := Hash("correct horse battery")
	if h == h2 {
		t.Error("two hashes of the same passphrase are identical; salt is not random")
	}
}

func TestHashRejectsShortPassphrase(t *testing.T) {
	if _, err := Hash("short"); err == nil {
		t.Error("short passphrase accepted")
	}
}

func TestVerifierRejectsBadHashes(t *testing.T) {
	good, _ := Hash("correct horse battery")
	parts := strings.Split(good, "$")
	bad := []string{
		"",
		"plaintext-password",
		strings.Replace(good, "argon2id", "argon2i", 1),
		strings.Replace(good, "v=19", "v=16", 1),
		// A tampered file must not make each login allocate 4 GiB.
		strings.Replace(good, "m=65536", "m=4194304", 1),
		strings.Replace(good, "t=3", "t=1000", 1),
		"$argon2id$v=19$m=65536,t=3,p=1$" + "$" + parts[5],
	}
	for _, h := range bad {
		if _, err := NewVerifier(h); err == nil {
			t.Errorf("NewVerifier(%q) accepted a bad hash", h)
		}
	}
}

func TestSessionLifetimes(t *testing.T) {
	c := newClock()
	s := NewSessions(c.now)
	tok, _ := s.Create()
	if !s.Valid(tok) || s.Valid("") || s.Valid("nope") {
		t.Fatal("basic validity wrong")
	}

	c.add(IdleTimeout - time.Minute)
	if !s.Valid(tok) {
		t.Fatal("expired before idle timeout")
	}
	c.add(IdleTimeout + time.Minute)
	if s.Valid(tok) {
		t.Fatal("survived idle timeout")
	}

	// Constant use can't stretch a session past its absolute lifetime.
	tok, _ = s.Create()
	for elapsed := time.Duration(0); elapsed < AbsoluteTimeout; elapsed += time.Hour {
		c.add(time.Hour)
		s.Valid(tok)
	}
	c.add(time.Hour)
	if s.Valid(tok) {
		t.Fatal("survived absolute timeout")
	}

	tok, _ = s.Create()
	s.Revoke(tok)
	if s.Valid(tok) {
		t.Fatal("revoked session still valid")
	}
}

func TestSessionStoreIsBounded(t *testing.T) {
	c := newClock()
	s := NewSessions(c.now)
	first, _ := s.Create()
	for i := 0; i < maxSessions; i++ {
		c.add(time.Second)
		s.Create()
	}
	if len(s.m) > maxSessions {
		t.Errorf("store grew to %d", len(s.m))
	}
	if s.Valid(first) {
		t.Error("oldest session was not evicted")
	}
}

func TestLimiter(t *testing.T) {
	c := newClock()
	l := NewLimiter(c.now)
	for i := 0; i < freeFailures-1; i++ {
		l.Fail("a")
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("locked out after %d failures", i+1)
		}
	}
	l.Fail("a")
	ok, wait := l.Allow("a")
	if ok || wait <= 0 {
		t.Fatal("no lockout after free failures")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("other clients locked out")
	}

	// Lockouts double and cap.
	var last time.Duration
	for i := 0; i < 20; i++ {
		c.add(maxLockout)
		l.Fail("a")
		_, w := l.Allow("a")
		if w < last && w != maxLockout {
			t.Fatalf("lockout shrank: %v after %v", w, last)
		}
		if w > maxLockout {
			t.Fatalf("lockout %v exceeds cap", w)
		}
		last = w
	}

	l.Succeed("a")
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("success did not clear lockout")
	}
}

func TestGlobalCapStopsIPRotation(t *testing.T) {
	c := newClock()
	l := NewLimiter(c.now)
	for i := 0; i < globalFailCap; i++ {
		l.Fail(string(rune('A'+i%26)) + string(rune('a'+i/26)))
	}
	if ok, _ := l.Allow("fresh-ip"); ok {
		t.Fatal("fresh client allowed past global cap")
	}
	c.add(globalWindow + time.Second)
	if ok, _ := l.Allow("fresh-ip"); !ok {
		t.Fatal("global cap did not expire")
	}
}

func TestClientIPTrustsOnlyConfiguredProxies(t *testing.T) {
	g, err := NewGate(nil, []string{"172.18.0.0/16", "10.0.0.7"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		remote, xff, want string
	}{
		{"192.168.0.50:4000", "", "192.168.0.50"},
		{"192.168.0.50:4000", "1.2.3.4", "192.168.0.50"}, // spoof ignored
		{"172.18.0.5:4000", "1.2.3.4", "1.2.3.4"},
		{"172.18.0.5:4000", "6.6.6.6, 1.2.3.4", "1.2.3.4"}, // left side is client-controlled
		{"172.18.0.5:4000", "1.2.3.4, 10.0.0.7", "1.2.3.4"},
		{"172.18.0.5:4000", "", "172.18.0.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := g.ClientIP(r); got != c.want {
			t.Errorf("remote %s xff %q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
	if _, err := NewGate(nil, []string{"not-an-ip"}); err == nil {
		t.Error("bad proxy entry accepted")
	}
}
