package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"strata/internal/tlsutil"
)

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer ok.Close()
	_, port, _ := net.SplitHostPort(ok.Listener.Addr().String())
	if got := healthcheck(":" + port); got != 0 {
		t.Fatalf("healthy server: got %d, want 0", got)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	_, port, _ = net.SplitHostPort(bad.Listener.Addr().String())
	if got := healthcheck("127.0.0.1:" + port); got != 1 {
		t.Fatalf("unhealthy server: got %d, want 1", got)
	}

	if got := healthcheck("not-an-addr"); got != 1 {
		t.Fatalf("bad addr: got %d, want 1", got)
	}
}

func TestHealthcheckPinsOwnCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	certFile := filepath.Join(t.TempDir(), "cert.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	t.Setenv("STRATA_TLS_CERT", certFile)
	if got := healthcheck(":" + port); got != 0 {
		t.Fatalf("pinned TLS probe: got %d, want 0", got)
	}

	// A server with a different certificate must not be trusted. (All
	// httptest TLS servers share one cert, so give this one its own.)
	cfg, _, err := tlsutil.Config("on", "", "", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	other := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	other.TLS = cfg
	other.StartTLS()
	defer other.Close()
	_, otherPort, _ := net.SplitHostPort(other.Listener.Addr().String())
	if got := healthcheck(":" + otherPort); got != 1 {
		t.Fatalf("probe trusted a certificate it was not pinned to: got %d", got)
	}
}

// TrueNAS hands apps whatever certificate the user picked, expired or not;
// the probe must still recognise its own server.
func TestHealthcheckAcceptsOwnExpiredCertificate(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "nas.example"},
		NotBefore: time.Now().AddDate(-3, 0, 0), NotAfter: time.Now().AddDate(-1, 0, 0),
		DNSNames: []string{"nas.example"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	certFile := filepath.Join(t.TempDir(), "cert.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	t.Setenv("STRATA_TLS_CERT", certFile)
	if got := healthcheck(":" + port); got != 0 {
		t.Fatalf("expired but pinned certificate: got %d, want 0", got)
	}
}
