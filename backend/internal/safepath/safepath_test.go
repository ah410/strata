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

func TestClean(t *testing.T) {
	cases := map[string]string{
		"":                        ".",
		"/":                       ".",
		"docs":                    "docs",
		"/docs/a.txt":             "docs/a.txt",
		"./docs//a.txt":           "docs/a.txt",
		"../":                     ".",
		"../../etc/passwd":        "etc/passwd",
		"docs/../../..":           ".",
		"/../../../../etc/shadow": "etc/shadow",
		`..\..\windows\system32`:  "windows/system32",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenInsideRoot(t *testing.T) {
	r, _ := newTestRoot(t)
	for _, in := range []string{"docs/a.txt", "/docs/a.txt", "../docs/a.txt"} {
		f, err := r.FS().Open(Clean(in))
		if err != nil {
			t.Errorf("Open(%q) = %v, want success", in, err)
			continue
		}
		f.Close()
	}
}

// The realistic NAS case: someone drops a symlink into the share that
// points at a dataset the app was never meant to serve.
func TestSymlinkEscapeIsBlocked(t *testing.T) {
	r, dir := newTestRoot(t)

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("classified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "docs", "link.txt")); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"escape/secret.txt", "docs/link.txt"} {
		if f, err := r.FS().Open(Clean(p)); err == nil {
			f.Close()
			t.Errorf("Open(%q) followed a symlink out of the root", p)
		} else if !errors.Is(Err(err), ErrEscape) {
			t.Errorf("Open(%q) = %v, want ErrEscape", p, err)
		}
	}
	if err := r.FS().WriteFile(Clean("escape/planted.txt"), []byte("x"), 0o644); err == nil {
		t.Error("write through an escaping symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Error("file landed outside the root")
	}
}

// A symlink that stays inside the share is fine to follow.
func TestSymlinkInsideRootWorks(t *testing.T) {
	r, dir := newTestRoot(t)
	if err := os.Symlink("docs", filepath.Join(dir, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	f, err := r.FS().Open("alias/a.txt")
	if err != nil {
		t.Fatalf("Open via in-root symlink: %v", err)
	}
	f.Close()
}

// The race the old check-then-use design lost: a directory that was fine
// when the request arrived is swapped for a symlink before the write.
func TestDirectorySwappedForSymlink(t *testing.T) {
	r, dir := newTestRoot(t)
	outside := t.TempDir()

	if _, err := r.FS().Stat("docs"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := r.FS().WriteFile("docs/planted.txt", []byte("x"), 0o644); err == nil {
		t.Error("write followed the swapped-in symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Fatal("file landed outside the root")
	}
}

func TestUserRefusesInternalDir(t *testing.T) {
	for _, p := range []string{".strata", "/.strata/trash/x", "docs/../.strata", `.strata\versions`, "media/.strata/trash", "../.strata"} {
		if _, err := User(p); !errors.Is(err, ErrReserved) || !errors.Is(err, ErrEscape) {
			t.Errorf("User(%q) = %v, want ErrReserved", p, err)
		}
	}
	for _, p := range []string{"", "docs", ".strata-notes", "x/.stratafile", "a/b"} {
		if _, err := User(p); err != nil {
			t.Errorf("User(%q) = %v, want ok", p, err)
		}
	}
}
