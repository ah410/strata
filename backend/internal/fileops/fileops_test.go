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
