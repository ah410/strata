// Package web holds the HTTP guards every request passes through before it
// reaches a handler: Host validation, cross-site request rejection, and
// security headers.
package web

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// CSRFHeader must be present on every state-changing request. Browsers only
// let a page add custom headers to cross-origin requests after a CORS
// preflight, which Strata never approves.
const CSRFHeader = "X-Strata"

// AppCSP is the policy for the UI and API. Inline styles stay allowed because
// React writes style attributes; scripts are same-origin files only.
const AppCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' blob: data:; media-src 'self'; frame-src 'self'; connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Guard wraps the app handler. allowed lists extra hostnames (without port)
// that may appear in the Host header; IP literals and localhost always pass.
func Guard(next http.Handler, allowed []string) http.Handler {
	hosts := map[string]bool{"localhost": true}
	for _, h := range allowed {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts[h] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", AppCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()")

		// DNS rebinding: an attacker's domain resolving to the NAS arrives
		// with that domain in Host. Only names we were told about pass.
		if !hostAllowed(r.Host, hosts) {
			log.Printf("blocked host %q from %s", r.Host, r.RemoteAddr)
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}

		if !safeMethod(r.Method) {
			if reason := crossSite(r); reason != "" {
				log.Printf("blocked %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, reason)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func hostAllowed(hostport string, hosts map[string]bool) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		return false
	}
	return net.ParseIP(host) != nil || hosts[host]
}

// crossSite returns why a state-changing request looks forged, or "".
func crossSite(r *http.Request) string {
	if r.Header.Get(CSRFHeader) != "1" {
		return "missing " + CSRFHeader + " header"
	}
	// Fetch metadata is set by every current browser and can't be forged by
	// page script; trust it when present.
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return "Sec-Fetch-Site " + r.Header.Get("Sec-Fetch-Site")
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return "origin " + o + " does not match host " + r.Host
		}
	}
	return ""
}

// RawHeaders sets the headers for serving a user file. Files are untrusted:
// anything a browser could execute is sandboxed or downgraded to text.
// It returns the Content-Type to use.
func RawHeaders(w http.ResponseWriter, ext string, detected string) string {
	h := w.Header()
	ext = strings.ToLower(ext)

	// The app iframes PDFs, so raw responses may be framed by Strata itself.
	h.Set("X-Frame-Options", "SAMEORIGIN")

	if ext == ".pdf" {
		// Chrome's PDF viewer refuses to run in a sandboxed document, and
		// PDF scripts are already confined by the viewer.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
		return "application/pdf"
	}

	h.Set("Content-Security-Policy",
		"default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'; sandbox; frame-ancestors 'self'")

	if inlineTypes[ext] != "" {
		return inlineTypes[ext]
	}
	if strings.HasPrefix(detected, "text/") || active[ext] {
		// HTML, SVG, scripts and friends are shown as source, never run.
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// inlineTypes are media types browsers render without executing anything.
var inlineTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp", ".avif": "image/avif",
	// SVG keeps its type so <img> previews work; <img> never runs SVG script,
	// and the sandbox CSP stops it when the file is opened directly.
	".svg": "image/svg+xml",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska",
	".mp3": "audio/mpeg", ".flac": "audio/flac", ".wav": "audio/wav", ".m4a": "audio/mp4",
	".ogg": "audio/ogg", ".opus": "audio/ogg",
	".txt": "text/plain; charset=utf-8", ".csv": "text/plain; charset=utf-8",
}

// active are formats a browser would execute or interpret as a document.
var active = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true, ".xml": true,
	".js": true, ".mjs": true, ".css": true, ".xsl": true, ".xslt": true,
}
