package auth

import (
	"crypto/sha256"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// basicTTL is how long a passphrase checked over Basic auth is remembered.
// File managers send it with every request, and argon2id is deliberately
// slow, so each one can't be verified from scratch.
const basicTTL = 10 * time.Minute

type basicCache struct {
	mu sync.Mutex
	ok map[[32]byte]time.Time
}

func (c *basicCache) hit(k [32]byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.ok[k]
	return ok && time.Since(t) < basicTTL
}

func (c *basicCache) put(k [32]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok == nil || len(c.ok) > 64 {
		c.ok = map[[32]byte]time.Time{}
	}
	c.ok[k] = time.Now()
}

// Basic guards a handler with HTTP Basic auth against the same passphrase
// as the web UI; the user name is ignored. It's for clients that can't do
// the cookie sign-in, like a mounted WebDAV drive, and it shares the
// sign-in throttle and HTTPS requirement.
func (g *Gate) Basic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.needsSetup() {
			http.Error(w, "finish setup in the web UI first", http.StatusPreconditionRequired)
			return
		}
		if !g.secure(r) {
			http.Error(w, "WebDAV needs HTTPS", http.StatusForbidden)
			return
		}
		_, pass, ok := r.BasicAuth()
		key := sha256.Sum256([]byte(pass))
		if ok && pass != "" && g.basic.hit(key) {
			next.ServeHTTP(w, r)
			return
		}
		client := g.ClientIP(r)
		if allowed, wait := g.limiter.Allow(client); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
			return
		}
		if !ok || pass == "" {
			// No credentials yet: ask, without counting it as a failure.
			challenge(w)
			return
		}
		if !g.verifier.Load().Verify(pass) {
			g.limiter.Fail(client)
			log.Printf("webdav sign-in failed from %s", client)
			challenge(w)
			return
		}
		g.limiter.Succeed(client)
		g.basic.put(key)
		next.ServeHTTP(w, r)
	})
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Strata", charset="UTF-8"`)
	http.Error(w, "sign in required", http.StatusUnauthorized)
}
