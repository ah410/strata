package auth

import (
	"encoding/json"
	"log"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// CookieName uses the __Host- prefix: the browser only accepts it with
// Secure, Path=/ and no Domain, so a sibling subdomain can't plant or read it.
const CookieName = "__Host-strata"

// Gate wires the passphrase check, sessions and throttling into HTTP.
type Gate struct {
	// verifier is nil until a passphrase exists (first-run setup mode).
	verifier atomic.Pointer[Verifier]
	sessions *Sessions
	limiter  *Limiter
	trusted  []netip.Prefix
	setup    *setup
}

// NewGate builds a Gate. trusted lists proxy addresses or CIDRs whose
// X-Forwarded-For and X-Forwarded-Proto headers are believed.
func NewGate(v *Verifier, trusted []string) (*Gate, error) {
	g := &Gate{sessions: NewSessions(nil), limiter: NewLimiter(nil)}
	if v != nil {
		g.verifier.Store(v)
	}
	for _, t := range trusted {
		if t = strings.TrimSpace(t); t == "" {
			continue
		}
		p, err := netip.ParsePrefix(t)
		if err != nil {
			a, aerr := netip.ParseAddr(t)
			if aerr != nil {
				return nil, err
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		g.trusted = append(g.trusted, p.Masked())
	}
	return g, nil
}

// public lists the API routes reachable without a session.
var public = map[string]bool{
	"/api/health":  true,
	"/api/login":   true,
	"/api/session": true,
	"/api/setup":   true,
}

// Require rejects /api requests without a valid session. The UI shell and
// its static assets stay public; they hold no data.
func (g *Gate) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !public[r.URL.Path] {
			if g.needsSetup() {
				http.Error(w, "setup required", http.StatusPreconditionRequired)
				return
			}
			if !g.authed(r) {
				http.Error(w, "sign in required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gate) authed(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	return err == nil && g.sessions.Valid(c.Value)
}

// Session answers whether the caller is signed in, for the UI's first load.
func (g *Gate) Session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if g.needsSetup() {
		http.Error(w, "setup required", http.StatusPreconditionRequired)
		return
	}
	if !g.authed(r) {
		http.Error(w, "sign in required", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}` + "\n"))
}

// Login checks the passphrase and issues a session cookie.
func (g *Gate) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	client := g.ClientIP(r)

	if g.needsSetup() {
		http.Error(w, "setup required", http.StatusPreconditionRequired)
		return
	}
	if !g.precheck(w, r, client) {
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
	}
	if !decodeSmall(w, r, &body) {
		return
	}

	if body.Passphrase == "" || !g.verifier.Load().Verify(body.Passphrase) {
		g.limiter.Fail(client)
		log.Printf("login failed from %s", client)
		http.Error(w, "wrong passphrase", http.StatusUnauthorized)
		return
	}
	g.limiter.Succeed(client)
	if g.startSession(w, r) {
		log.Printf("login ok from %s", client)
	}
}

// precheck applies the HTTPS requirement, throttling and content type shared
// by sign-in and setup. It writes the error response itself.
func (g *Gate) precheck(w http.ResponseWriter, r *http.Request, client string) bool {
	if !g.secure(r) {
		http.Error(w, "sign-in needs HTTPS; open Strata through its https:// address", http.StatusForbidden)
		return false
	}
	if ok, wait := g.limiter.Allow(client); !ok {
		log.Printf("sign-in throttled for %s (%s left)", client, wait.Round(time.Second))
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return false
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

func decodeSmall(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(v); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return false
	}
	return true
}

// startSession issues a fresh session cookie. Any cookie the client brought
// is revoked first, so a planted session ID can't be carried over.
func (g *Gate) startSession(w http.ResponseWriter, r *http.Request) bool {
	if c, err := r.Cookie(CookieName); err == nil {
		g.sessions.Revoke(c.Value)
	}
	token, err := g.sessions.Create()
	if err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(AbsoluteTimeout.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}` + "\n"))
	return true
}

// Logout ends the caller's session and clears the cookie.
func (g *Gate) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		g.sessions.Revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}` + "\n"))
}

// ClientIP is the address throttling and logs refer to. Forwarded headers
// count only when the direct peer is a trusted proxy; otherwise anyone could
// claim a fresh address on each guess.
func (g *Gate) ClientIP(r *http.Request) string {
	peer := remoteAddr(r)
	if !g.isTrusted(peer) {
		return peer.String()
	}
	// Walk right to left: the rightmost untrusted hop is the real client.
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !g.isTrusted(a.Unmap()) {
			return a.Unmap().String()
		}
	}
	return peer.String()
}

// secure reports whether the browser reached us over HTTPS, either directly,
// via a trusted proxy that terminated TLS, or on loopback for development.
func (g *Gate) secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := remoteAddr(r)
	if g.isTrusted(peer) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return peer.IsLoopback() && r.Header.Get("X-Forwarded-For") == ""
}

func (g *Gate) isTrusted(a netip.Addr) bool {
	for _, p := range g.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}
