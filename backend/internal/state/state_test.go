package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistsAndFollowsMoves(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFavorite("Docs", true)
	s.SetFavorite("Docs/a.md", true)
	s.SetFavorite("Docs", true) // no duplicate
	s.Touch("Docs/a.md")
	s.Touch("b.txt")
	s.Touch("Docs/a.md") // moves to front
	s.Rename("Docs", "Archive/Docs")
	s.Rename("Docsish", "x") // a sibling prefix is not a child

	s2, _ := Open(dir)
	favs, recent := s2.Snapshot()
	if len(favs) != 2 || favs[0] != "Archive/Docs" || favs[1] != "Archive/Docs/a.md" {
		t.Fatalf("favorites %v", favs)
	}
	if len(recent) != 2 || recent[0].Path != "Archive/Docs/a.md" || recent[1].Path != "b.txt" {
		t.Fatalf("recent %v", recent)
	}
	s2.SetFavorite("Archive/Docs", false)
	if f, _ := s2.Snapshot(); len(f) != 1 {
		t.Fatalf("after unstar %v", f)
	}
	for range MaxRecent + 5 {
		s2.Touch(filepath.Join("f", t.Name()+string(rune('a'+len(recent)))))
		recent = append(recent, Recent{})
	}
	if _, r := s2.Snapshot(); len(r) > MaxRecent {
		t.Fatalf("recent grew to %d", len(r))
	}
}

func TestDamagedFileIsSetAside(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "state.json"), []byte("{nope"), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Snapshot(); len(f) != 0 {
		t.Fatal("damaged state loaded")
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json.bad")); err != nil {
		t.Fatal("damaged file not kept aside")
	}
	var nilStore *Store
	if nilStore.Enabled() || nilStore.Touch("x") != nil {
		t.Fatal("nil store should be a quiet no-op")
	}
}
