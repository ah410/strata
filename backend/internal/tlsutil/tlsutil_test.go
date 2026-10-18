package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestSelfSignedIsPersistedAndReused(t *testing.T) {
	dir := t.TempDir()
	c1, fp1, err := Config("auto", "", "", dir, []string{"strata.example.com", "192.168.0.78"})
	if err != nil {
		t.Fatal(err)
	}
	_, fp2, err := Config("auto", "", "", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Error("certificate changed across restarts")
	}
	if c1.MinVersion != tls.VersionTLS12 {
		t.Error("TLS minimum below 1.2")
	}

	leaf, _ := x509.ParseCertificate(c1.Certificates[0].Certificate[0])
	if err := leaf.VerifyHostname("strata.example.com"); err != nil {
		t.Errorf("SAN missing hostname: %v", err)
	}
	if err := leaf.VerifyHostname("192.168.0.78"); err != nil {
		t.Errorf("SAN missing IP: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "tls", "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("private key mode %v, want owner-only", info.Mode().Perm())
	}
}

func TestProvidedPairAndModes(t *testing.T) {
	dir := t.TempDir()
	_, fp, _ := Config("on", "", "", dir, nil)
	cert, key := filepath.Join(dir, "tls", "cert.pem"), filepath.Join(dir, "tls", "key.pem")
	if _, fp2, err := Config("auto", cert, key, "", nil); err != nil || fp2 != fp {
		t.Errorf("provided pair: %v, fingerprint match %v", err, fp2 == fp)
	}
	if _, _, err := Config("auto", cert, "", "", nil); err == nil {
		t.Error("cert without key accepted")
	}
	if c, _, err := Config("off", "", "", dir, nil); c != nil || err != nil {
		t.Error("off mode returned a config")
	}
	if _, _, err := Config("maybe", "", "", dir, nil); err == nil {
		t.Error("unknown mode accepted")
	}
	t.Setenv("TMPDIR", t.TempDir())
	if c, _, err := Config("auto", "", "", "", nil); err != nil || c == nil {
		t.Error("no config dir should still give an in-memory pair")
	}
}
