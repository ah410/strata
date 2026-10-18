// Package tlsutil gives Strata HTTPS without a reverse proxy: a provided
// certificate, or a self-signed one generated once and kept in the config
// directory so its fingerprint stays stable across restarts.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config returns a server TLS config, or nil when mode is "off". The caller
// decides what "auto" means; here it behaves like "on".
// certFile/keyFile win when set; otherwise a self-signed pair is loaded from,
// or created in, dir. An empty dir means a throwaway in-memory pair.
func Config(mode, certFile, keyFile, dir string, hosts []string) (*tls.Config, string, error) {
	switch mode {
	case "off":
		return nil, "", nil
	case "auto", "on", "":
	default:
		return nil, "", fmt.Errorf(`STRATA_TLS must be "auto", "on" or "off", got %q`, mode)
	}

	var cert tls.Certificate
	var err error
	switch {
	case certFile != "" || keyFile != "":
		if certFile == "" || keyFile == "" {
			return nil, "", errors.New("STRATA_TLS_CERT and STRATA_TLS_KEY must be set together")
		}
		cert, err = tls.LoadX509KeyPair(certFile, keyFile)
	case dir != "":
		cert, err = selfSigned(filepath.Join(dir, "tls"), hosts)
	default:
		var certPEM, keyPEM []byte
		if certPEM, keyPEM, err = generate(hosts); err == nil {
			cert, err = tls.X509KeyPair(certPEM, keyPEM)
		}
	}
	if err != nil {
		return nil, "", err
	}

	sum := sha256.Sum256(cert.Certificate[0])
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, fingerprint(sum[:]), nil
}

// selfSigned loads dir/cert.pem and dir/key.pem, creating them if absent.
func selfSigned(dir string, hosts []string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return c, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return tls.Certificate{}, fmt.Errorf("stored TLS pair: %w", err)
	}

	certPEM, keyPEM, err := generate(hosts)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}
	// Key first: a crash between the writes leaves no cert pointing at a
	// missing key, and the next start simply regenerates.
	if err := writePrivate(keyPath, keyPEM); err != nil {
		return tls.Certificate{}, err
	}
	if err := writePrivate(certPath, certPEM); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func generate(hosts []string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Strata (self-signed)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kder, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}), nil
}

func writePrivate(path string, data []byte) error {
	// CreateTemp already uses mode 0600; no chmod, which ZFS
	// aclmode=restricted would reject on an ACL'd dataset.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// fingerprint formats a SHA-256 digest the way browsers show it.
func fingerprint(sum []byte) string {
	h := strings.ToUpper(hex.EncodeToString(sum))
	var b strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(h[i : i+2])
	}
	return b.String()
}
