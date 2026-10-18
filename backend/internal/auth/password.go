// Package auth gates Strata behind a single passphrase: argon2id hashing,
// in-memory sessions, and login throttling.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. 64 MiB per attempt makes offline guessing expensive;
// Verifier limits concurrency so attempts can't exhaust the container.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16

	// MinPassphrase is enforced when hashing, not when verifying.
	MinPassphrase = 12
)

var b64 = base64.RawStdEncoding

// Hash returns a PHC-format argon2id hash of passphrase.
func Hash(passphrase string) (string, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return "", fmt.Errorf("passphrase must be at least %d characters", MinPassphrase)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

type params struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	key     []byte
}

// parse reads a PHC argon2id string, bounding parameters so a tampered
// hash file can't make every login allocate gigabytes.
func parse(phc string) (*params, error) {
	parts := strings.Split(strings.TrimSpace(phc), "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, errors.New("not an argon2id hash")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return nil, errors.New("unsupported argon2 version")
	}
	p := &params{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return nil, errors.New("bad argon2 parameters")
	}
	if p.memory < 19*1024 || p.memory > 256*1024 || p.time < 1 || p.time > 10 || p.threads < 1 || p.threads > 4 {
		return nil, errors.New("argon2 parameters out of range")
	}
	var err error
	if p.salt, err = b64.DecodeString(parts[4]); err != nil || len(p.salt) < 8 {
		return nil, errors.New("bad salt")
	}
	if p.key, err = b64.DecodeString(parts[5]); err != nil || len(p.key) < 16 {
		return nil, errors.New("bad key")
	}
	return p, nil
}

// Verifier checks passphrases against one stored hash.
type Verifier struct {
	p   *params
	sem chan struct{}
}

// NewVerifier parses the stored hash up front, so a bad file fails startup
// rather than every login.
func NewVerifier(phc string) (*Verifier, error) {
	p, err := parse(phc)
	if err != nil {
		return nil, err
	}
	// Two hashes in flight at most: 128 MiB worst case, whatever the load.
	return &Verifier{p: p, sem: make(chan struct{}, 2)}, nil
}

// Verify reports whether passphrase matches, in constant time.
func (v *Verifier) Verify(passphrase string) bool {
	v.sem <- struct{}{}
	defer func() { <-v.sem }()
	key := argon2.IDKey([]byte(passphrase), v.p.salt, v.p.time, v.p.memory, v.p.threads, uint32(len(v.p.key)))
	return subtle.ConstantTimeCompare(key, v.p.key) == 1
}
