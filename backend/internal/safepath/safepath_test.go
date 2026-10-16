package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestRoot(t *testing.T) (*Root, string) {
	t.Helper()
	// t.TempDir on macOS lives under /var -> /private/var, so the root must
	// resolve symlinks for comparisons to hold.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestResolveAllowsPathsInsideRoot(t *testing.T) {
	r, _ := newTestRoot(t)

	for _, in := range []string{"", "/", "docs", "/docs", "docs/a.txt", "./docs/a.txt", "docs//a.txt"} {
		if _, err := r.Resolve(in); err != nil {
			t.Errorf("Resolve(%q) = %v, want success", in, err)
		}
	}
}

func TestResolveBlocksTraversal(t *testing.T) {
	r, _ := newTestRoot(t)

	attacks := []string{
		"../",
		"../../etc/passwd",
		"docs/../../..",
		"/../../../../etc/shadow",
		`..\..\windows\system32`,
		"docs/../../outside.txt",
	}
	for _, in := range attacks {
		got, err := r.Resolve(in)
		if err == nil {
			// Clean() may neutralise some of these into the root itself,
			// which is safe; what matters is that nothing lands outside.
			if !within(r.Path(), got) {
				t.Errorf("Resolve(%q) = %q, which escapes root %q", in, got, r.Path())
			}
			continue
		}
		if !errors.Is(err, ErrEscape) {
			t.Errorf("Resolve(%q) = %v, want ErrEscape", in, err)
		}
	}
}

func TestResolveBlocksSymlinkEscape(t *testing.T) {
	r, dir := newTestRoot(t)

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("classified"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The realistic NAS case: someone drops a symlink into the share that
	// points at a dataset the app was never meant to serve.
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := r.Resolve("escape/secret.txt"); !errors.Is(err, ErrEscape) {
		t.Errorf("symlink traversal allowed: err = %v, want ErrEscape", err)
	}
	if _, err := r.Resolve("escape"); !errors.Is(err, ErrEscape) {
		t.Errorf("symlinked directory allowed: err = %v, want ErrEscape", err)
	}
}

func TestResolveAllowsNonexistentTargetForCreation(t *testing.T) {
	r, _ := newTestRoot(t)

	// Upload and mkdir resolve a path that does not exist yet; this must
	// succeed while still being bounded by the root.
	got, err := r.Resolve("docs/new/deep/file.txt")
	if err != nil {
		t.Fatalf("Resolve of new path failed: %v", err)
	}
	if !within(r.Path(), got) {
		t.Errorf("new path %q escapes root", got)
	}

	// A leading ".." on a path that does not exist yet is clamped into the
	// root by Clean rather than rejected. Either outcome is safe; what must
	// never happen is a resolved path outside the root.
	clamped, err := r.Resolve("../new/outside.txt")
	if err != nil {
		if !errors.Is(err, ErrEscape) {
			t.Errorf("unexpected error: %v", err)
		}
		return
	}
	if !within(r.Path(), clamped) {
		t.Errorf("Resolve escaped to %q, outside root %q", clamped, r.Path())
	}
}

func TestRel(t *testing.T) {
	r, _ := newTestRoot(t)

	abs, err := r.Resolve("docs/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Rel(abs); got != "docs/a.txt" {
		t.Errorf("Rel = %q, want docs/a.txt", got)
	}
	if got := r.Rel(r.Path()); got != "" {
		t.Errorf("Rel(root) = %q, want empty", got)
	}
}
