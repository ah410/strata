package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strata/internal/safepath"
)

func TestSaveWritesWithoutLeavingTempFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"first", "second"} {
		if err := Save(root, "sub/note.txt", strings.NewReader(body)); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "sub", "note.txt"))
	if err != nil || string(got) != "second" {
		t.Fatalf("content = %q, %v", got, err)
	}
	info, _ := os.Stat(filepath.Join(dir, "sub", "note.txt"))
	if info.Mode().Perm()&0o600 != 0o600 {
		t.Fatalf("mode = %v, owner cannot read/write", info.Mode())
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "sub", ".strata-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestCopySkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("classified"), 0o600)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "keep.txt"), []byte("ok"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "src", "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root, _ := safepath.NewRoot(dir)

	if err := Copy(root, "src", "dst"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "dst", "keep.txt")); err != nil || string(b) != "ok" {
		t.Errorf("regular file not copied: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "dst", "escape")); err == nil {
		t.Error("symlink was copied or followed")
	}
	if err := Copy(root, "src/escape", "linkcopy"); err == nil {
		t.Error("copying a symlink directly reported success")
	}
}

func TestRootIsProtected(t *testing.T) {
	dir := t.TempDir()
	root, _ := safepath.NewRoot(dir)
	for _, p := range []string{"", "/", "..", "a/../.."} {
		if err := Delete(root, p); err == nil {
			t.Errorf("Delete(%q) succeeded", p)
		}
		if err := Move(root, p, "elsewhere"); err == nil {
			t.Errorf("Move(%q) succeeded", p)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("root is gone")
	}
}

func TestSaveDoesNotFollowEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "up")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root, _ := safepath.NewRoot(dir)
	if err := Save(root, "up/planted.txt", strings.NewReader("x")); err == nil {
		t.Error("Save through escaping symlink succeeded")
	}
	if left, _ := filepath.Glob(filepath.Join(outside, "*")); len(left) != 0 {
		t.Errorf("files landed outside the root: %v", left)
	}
}

func TestListReportsRootAsEmptyPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	root, _ := safepath.NewRoot(dir)
	l, err := List(root, "/")
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != "" || len(l.Entries) != 1 || l.Entries[0].Path != "a.txt" {
		t.Errorf("listing = %+v", l)
	}
}
