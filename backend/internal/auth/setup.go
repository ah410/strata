package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// setup holds first-run state: a one-time code that proves the person
// setting the passphrase can read the app's logs, i.e. administers the NAS.
type setup struct {
	mu       sync.Mutex
	code     string
	hashPath string
}

// NewSetupGate starts in setup mode. The passphrase hash is written to
// hashPath once someone presents the code returned here.
func NewSetupGate(hashPath string, trusted []string) (*Gate, string, error) {
	g, err := NewGate(nil, trusted)
	if err != nil {
		return nil, "", err
	}
	raw := make([]byte, 13) // 104 bits
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)[:20]
	g.setup = &setup{code: code, hashPath: hashPath}
	return g, formatCode(code), nil
}

func (g *Gate) needsSetup() bool { return g.verifier.Load() == nil }

// Setup sets the passphrase on first run and signs the caller in.
func (g *Gate) Setup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	client := g.ClientIP(r)
	if g.setup == nil || !g.needsSetup() {
		http.Error(w, "already set up", http.StatusConflict)
		return
	}
	if !g.precheck(w, r, client) {
		return
	}
	var body struct {
		Code       string `json:"code"`
		Passphrase string `json:"passphrase"`
	}
	if !decodeSmall(w, r, &body) {
		return
	}

	s := g.setup
	s.mu.Lock()
	defer s.mu.Unlock()
	if !g.needsSetup() { // lost a race with another setup request
		http.Error(w, "already set up", http.StatusConflict)
		return
	}
	if subtle.ConstantTimeCompare([]byte(normalizeCode(body.Code)), []byte(s.code)) != 1 {
		g.limiter.Fail(client)
		log.Printf("setup: wrong code from %s", client)
		http.Error(w, "wrong setup code", http.StatusUnauthorized)
		return
	}

	h, err := Hash(body.Passphrase)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := NewVerifier(h)
	if err != nil {
		http.Error(w, "could not set passphrase", http.StatusInternalServerError)
		return
	}
	if err := writeHash(s.hashPath, h); err != nil {
		log.Printf("setup: saving passphrase: %v", err)
		http.Error(w, "could not save passphrase", http.StatusInternalServerError)
		return
	}
	g.verifier.Store(v)
	s.code = ""
	g.limiter.Succeed(client)
	log.Printf("setup complete from %s; passphrase saved to %s", client, s.hashPath)
	g.startSession(w, r)
}

// writeHash stores the hash atomically with owner-only permissions.
func writeHash(path, hash string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auth-*") // mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(hash + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func formatCode(c string) string {
	var b strings.Builder
	for i := 0; i < len(c); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(c[i:min(i+4, len(c))])
	}
	return b.String()
}

// normalizeCode accepts the code however it was typed: any case, with or
// without dashes and spaces.
func normalizeCode(c string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, c)
}
