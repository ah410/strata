// Package safepath resolves user-supplied paths against a fixed root and
// guarantees the result cannot escape that root.
//
// Every filesystem operation in this service MUST route through Resolve.
// The threat model is a request like ?path=../../etc/shadow, or a symlink
// inside the shared volume pointing at /, which is entirely plausible on a
// NAS where users drop arbitrary files into the dataset.
package safepath

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrEscape is returned when a requested path resolves outside the root.
var ErrEscape = errors.New("path escapes root")

// Root is an absolute, symlink-resolved directory that bounds all access.
type Root struct {
	abs string
}

// NewRoot resolves dir to an absolute path with symlinks evaluated, so that
// later comparisons are made against the real location on disk.
func NewRoot(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// EvalSymlinks fails if the directory does not exist yet; that is a
	// configuration error worth surfacing loudly at startup rather than
	// silently serving a path that later becomes a symlink.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return &Root{abs: real}, nil
}

// Path returns the root's absolute location.
func (r *Root) Path() string { return r.abs }

// Resolve maps a request-supplied relative path to an absolute path inside
// the root. The returned path is safe to hand to os.Open, os.Stat, etc.
//
// The caller's path is treated as relative to the root regardless of whether
// it carries a leading slash, so "/foo" and "foo" mean the same thing.
func (r *Root) Resolve(rel string) (string, error) {
	// Normalize separators coming from a browser on Windows.
	rel = strings.ReplaceAll(rel, "\\", "/")

	// filepath.Clean collapses ".." segments, but a path that begins with
	// ".." still escapes, so joining alone is not sufficient.
	clean := filepath.Clean("/" + rel)
	joined := filepath.Join(r.abs, clean)

	// Defence in depth: confirm the lexical result is still under root.
	if !within(r.abs, joined) {
		return "", ErrEscape
	}

	// A component of the path may be a symlink pointing outside the root.
	// Resolve what exists and re-check. The target may legitimately not
	// exist yet (upload, mkdir), in which case we verify the deepest
	// existing ancestor instead.
	resolved, err := resolveExisting(joined)
	if err != nil {
		return "", err
	}
	if !within(r.abs, resolved) {
		return "", ErrEscape
	}

	return joined, nil
}

// resolveExisting walks up from p until it finds a path that exists, resolves
// that through symlinks, and re-appends the trailing components.
func resolveExisting(p string) (string, error) {
	missing := ""
	cur := p
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if missing == "" {
				return real, nil
			}
			return filepath.Join(real, missing), nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the filesystem root without finding anything.
			return "", ErrEscape
		}
		missing = filepath.Join(filepath.Base(cur), missing)
		cur = parent
	}
}

// within reports whether child is root itself or lies beneath it.
func within(root, child string) bool {
	if child == root {
		return true
	}
	return strings.HasPrefix(child, root+string(filepath.Separator))
}

// Rel converts an absolute path inside the root back to the forward-slash
// relative form the API speaks. It returns "" for the root itself.
func (r *Root) Rel(abs string) string {
	rel, err := filepath.Rel(r.abs, abs)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
