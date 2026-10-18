package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// Session lifetimes. Sessions live in memory only, so a restart signs
// everyone out; that is also how a passphrase change takes effect.
const (
	IdleTimeout     = 12 * time.Hour
	AbsoluteTimeout = 7 * 24 * time.Hour
	maxSessions     = 256
)

type session struct {
	created  time.Time
	lastSeen time.Time
}

// Sessions is an in-memory session store keyed by a hash of the token, so
// the raw tokens never sit in this process's memory after issue.
type Sessions struct {
	mu  sync.Mutex
	m   map[[32]byte]*session
	now func() time.Time
}

func NewSessions(now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{m: map[[32]byte]*session{}, now: now}
}

// Create issues a new 256-bit token.
func (s *Sessions) Create() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	if len(s.m) >= maxSessions {
		s.evictOldest()
	}
	t := s.now()
	s.m[sha256.Sum256([]byte(token))] = &session{created: t, lastSeen: t}
	return token, nil
}

// Valid reports whether token is live, and extends its idle window.
func (s *Sessions) Valid(token string) bool {
	if token == "" {
		return false
	}
	k := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.m[k]
	if !ok {
		return false
	}
	t := s.now()
	if expired(ss, t) {
		delete(s.m, k)
		return false
	}
	ss.lastSeen = t
	return true
}

// Revoke ends a session (logout).
func (s *Sessions) Revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sha256.Sum256([]byte(token)))
}

func expired(ss *session, t time.Time) bool {
	return t.Sub(ss.lastSeen) > IdleTimeout || t.Sub(ss.created) > AbsoluteTimeout
}

func (s *Sessions) sweep() {
	t := s.now()
	for k, ss := range s.m {
		if expired(ss, t) {
			delete(s.m, k)
		}
	}
}

func (s *Sessions) evictOldest() {
	var oldest [32]byte
	var at time.Time
	for k, ss := range s.m {
		if at.IsZero() || ss.lastSeen.Before(at) {
			oldest, at = k, ss.lastSeen
		}
	}
	delete(s.m, oldest)
}
