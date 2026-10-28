package fileops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuplicates(t *testing.T) {
	root, dir := share(t)
	big := strings.Repeat("a", 200_000)
	write(t, dir, "a/movie.mkv", big)
	write(t, dir, "b/movie copy.mkv", big)
	write(t, dir, "c/movie.mkv", big)
	// Same size and same first 64 KB, different tail: not a duplicate.
	write(t, dir, "d/near.mkv", big[:199_999]+"b")
	write(t, dir, "s/1.txt", "small")
	write(t, dir, "s/2.txt", "small")
	write(t, dir, "s/other.txt", "other")
	write(t, dir, ".strata/trash/x/item", big)
	write(t, dir, ".hidden/movie.mkv", big)
	os.Link(filepath.Join(dir, "a/movie.mkv"), filepath.Join(dir, "a/hardlink.mkv"))

	var last int64
	got, err := Duplicates(context.Background(), root, "", 1, func(d, _ int64) { last = d })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("groups = %+v", got)
	}
	g := got[0]
	// The hard link is the same inode, so it's one copy under either name.
	if len(g.Paths) != 3 || !strings.HasPrefix(g.Paths[0], "a/") || g.Paths[1] != "b/movie copy.mkv" || g.Wasted != 400_000 {
		t.Fatalf("first group = %+v", g)
	}
	if strings.Join(got[1].Paths, ",") != "s/1.txt,s/2.txt" {
		t.Fatalf("second group = %+v", got[1])
	}
	if last == 0 {
		t.Fatal("no progress")
	}

	// A minimum size skips small files; a folder limits the search.
	got, _ = Duplicates(context.Background(), root, "", 1000, nil)
	if len(got) != 1 {
		t.Fatalf("with min size: %+v", got)
	}
	got, _ = Duplicates(context.Background(), root, "s", 1, nil)
	if len(got) != 1 || got[0].Paths[0] != "s/1.txt" {
		t.Fatalf("in s: %+v", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Duplicates(ctx, root, "", 1, nil); err == nil {
		t.Fatal("canceled scan succeeded")
	}
}
