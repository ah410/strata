// Package safepath bounds every filesystem access to a fixed root.
//
// The threat model is a request like ?path=../../etc/shadow, or a symlink
// inside the shared volume pointing at /, which is entirely plausible on a
// NAS where users drop arbitrary files into the dataset over SMB.
//
// Paths are not checked and then used: every operation goes through an
// os.Root, which resolves each component relative to an open directory
// handle and refuses to leave it. A symlink swapped in between two calls
// cannot redirect an operation outside the root.
package safepath

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrEscape is returned when a requested path resolves outside the root.
var ErrEscape = errors.New("path escapes root")

// Internal is the directory inside the share where Strata keeps its trash,
// file versions and partial uploads. Requests may never name it directly.
const Internal = ".strata"

// ErrReserved is returned for request paths inside Internal.
var ErrReserved = fmt.Errorf("%w: reserved for Strata", ErrEscape)

// Root is the directory that bounds all access.
type Root struct {
	abs string
	fs  *os.Root
}

// NewRoot opens dir as the root. Symlinks in dir itself are resolved once,
// at startup, so the configured path may be a link.
func NewRoot(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	fs, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	return &Root{abs: real, fs: fs}, nil
}

// Path returns the root's absolute location, for logging and statfs only.
func (r *Root) Path() string { return r.abs }

// FS returns the handle every file operation must go through.
func (r *Root) FS() *os.Root { return r.fs }

// Clean turns a request-supplied path into the root-relative form os.Root
// expects: forward slashes, no "..", and "." for the root itself. A leading
// slash is ignored, so "/foo" and "foo" mean the same thing.
func Clean(rel string) string {
	// Normalize separators coming from a browser on Windows.
	rel = strings.ReplaceAll(rel, "\\", "/")
	// Cleaning against "/" clamps any ".." at the root instead of escaping.
	c := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if c == "" {
		return "."
	}
	return c
}

// User cleans a path that came from a request and refuses Strata's internal
// directory, so trash and version files are reachable only through their
// own endpoints.
func User(rel string) (string, error) {
	c := Clean(rel)
	// Any depth: a nested dataset keeps its own .strata for trash.
	for _, part := range strings.Split(c, "/") {
		if part == Internal {
			return "", ErrReserved
		}
	}
	return c, nil
}

// Err maps os.Root's escape refusal onto ErrEscape so callers can tell a
// blocked traversal from an ordinary failure.
func Err(err error) error {
	// os.Root's sentinel is unexported; its message is stable API text.
	if err != nil && strings.Contains(err.Error(), "path escapes from parent") {
		return ErrEscape
	}
	return err
}
