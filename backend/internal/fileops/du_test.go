package fileops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scanned(t *testing.T, s *Sizer, rel string) *SizeReport {
	t.Helper()
	for range 400 {
		r, err := s.Report(rel, false)
		if err != nil {
			t.Fatal(err)
		}
		if r.Ready && !r.Scanning {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("scan never finished")
	return nil
}

func TestSizerReportsFoldersBySize(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "Movies/a.mkv", strings.Repeat("m", 300_000))
	write(t, dir, "Movies/sub/b.mkv", strings.Repeat("m", 200_000))
	write(t, dir, "Docs/n.txt", strings.Repeat("d", 50_000))
	write(t, dir, "big.iso", strings.Repeat("i", 400_000))
	write(t, dir, ".cache/junk", strings.Repeat("h", 100_000))
	write(t, dir, ".strata/trash/x/item", strings.Repeat("t", 100_000))
	write(t, dir, ".strata/versions/v/1", strings.Repeat("v", 100_000))
	os.Symlink("/", filepath.Join(dir, "loop"))

	s := NewSizer(root, time.Hour)
	first, err := s.Report("", false)
	if err != nil || first.Ready || !first.Scanning {
		t.Fatalf("first report should start a scan: %+v %v", first, err)
	}
	r := scanned(t, s, "")

	var names []string
	for _, c := range r.Children {
		names = append(names, c.Name)
	}
	// Folder sizes include everything below; dotfiles and .strata are not rows.
	if strings.Join(names, ",") != "Movies,big.iso,Docs" {
		t.Fatalf("children = %v", names)
	}
	if r.Children[0].Size < 500_000 || r.Children[0].Files != 2 || !r.Children[0].IsDir {
		t.Fatalf("Movies = %+v", r.Children[0])
	}
	if r.Hidden < 100_000 || r.Trash < 100_000 || r.Versions < 100_000 {
		t.Fatalf("hidden %d trash %d versions %d", r.Hidden, r.Trash, r.Versions)
	}
	if r.Kinds["video"] < 500_000 || r.Kinds["text"] < 50_000 {
		t.Fatalf("kinds = %v", r.Kinds)
	}
	if len(r.Largest) != 4 || r.Largest[0].Path != "Movies/a.mkv" && r.Largest[0].Path != "big.iso" {
		t.Fatalf("largest = %+v", r.Largest)
	}

	// Drilling in reads the same scan.
	m := scanned(t, s, "Movies")
	if m.Path != "Movies" || len(m.Children) != 2 || m.Children[0].Name != "a.mkv" {
		t.Fatalf("Movies report = %+v", m)
	}
	for _, f := range m.Largest {
		if !strings.HasPrefix(f.Path, "Movies/") {
			t.Fatalf("largest outside folder: %s", f.Path)
		}
	}
}

func TestSizerCountsHardLinksOnce(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "a/f", strings.Repeat("x", 200_000))
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	if err := os.Link(filepath.Join(dir, "a/f"), filepath.Join(dir, "b/f")); err != nil {
		t.Skip("no hard links here")
	}
	r := scanned(t, NewSizer(root, time.Hour), "")
	if r.Size < 200_000 || r.Size >= 400_000 {
		t.Fatalf("total %d, want one copy of 200 KB", r.Size)
	}
}

func TestSizerRescanAndErrors(t *testing.T) {
	root, dir := share(t)
	s := NewSizer(root, time.Hour)
	scanned(t, s, "")
	write(t, dir, "new/f", "x")
	if _, err := s.Report("new", false); err == nil {
		t.Fatal("folder from after the scan should ask for a rescan")
	}
	s.Report("", true)
	scanned(t, s, "")
	if r := scanned(t, s, "new"); r.Files != 1 {
		t.Fatalf("after rescan: %+v", r)
	}
	if _, err := s.Report(".strata/trash", false); err == nil {
		t.Fatal("internal dir reachable")
	}
}
