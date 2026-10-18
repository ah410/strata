package fileops

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"strata/internal/safepath"
)

func share(t *testing.T) (*safepath.Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestTrashAndRestoreRoundTrip(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "docs/report.xlsx", "workbook bytes")
	write(t, dir, "folder/a.txt", "a")
	write(t, dir, "folder/sub/b.txt", "b")

	for _, p := range []string{"docs/report.xlsx", "folder"} {
		if _, err := Trash(root, p); err != nil {
			t.Fatalf("Trash(%s): %v", p, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, p)); err == nil {
			t.Fatalf("%s still present after trash", p)
		}
	}
	l, _ := List(root, "docs")
	if len(l.Entries) != 0 {
		t.Errorf("trashed file still listed: %+v", l.Entries)
	}
	items, err := ListTrash(root)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListTrash = %v, %v", items, err)
	}
	if items[0].Path != "folder" || !items[0].IsDir || items[1].Path != "docs/report.xlsx" || items[1].Size != 14 {
		t.Errorf("trash items wrong (want newest first): %+v", items)
	}

	for _, it := range items {
		if _, err := Restore(root, it.ID); err != nil {
			t.Fatalf("Restore(%s): %v", it.Path, err)
		}
	}
	if read(t, dir, "docs/report.xlsx") != "workbook bytes" || read(t, dir, "folder/sub/b.txt") != "b" {
		t.Error("restored content differs")
	}
	if items, _ := ListTrash(root); len(items) != 0 {
		t.Errorf("trash not empty after restore: %+v", items)
	}
}

func TestRestoreNeverOverwrites(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "notes.txt", "old")
	Trash(root, "notes.txt")
	write(t, dir, "notes.txt", "new")
	write(t, dir, "notes (restored).txt", "taken")
	items, _ := ListTrash(root)
	got, err := Restore(root, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "notes (restored 2).txt" || read(t, dir, got) != "old" || read(t, dir, "notes.txt") != "new" {
		t.Errorf("restore went to %q; contents wrong or overwritten", got)
	}
}

func TestTrashRefusals(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "a.txt", "x")
	for _, p := range []string{"", "/", ".strata", ".strata/trash", "missing.txt"} {
		if _, err := Trash(root, p); err == nil {
			t.Errorf("Trash(%q) succeeded", p)
		}
	}
	for _, id := range []string{"", "../../etc", "zzzzzzzzzzzzzzzzzzzzzzzz", "0123"} {
		if _, err := Restore(root, id); err == nil {
			t.Errorf("Restore(%q) succeeded", id)
		}
		if err := Purge(root, id); err == nil {
			t.Errorf("Purge(%q) succeeded", id)
		}
	}
}

// The trash record lives on disk where an SMB user could edit it; a forged
// path must not let a restore write outside the share or into .strata.
func TestTamperedTrashRecordIsContained(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "a.txt", "payload")
	Trash(root, "a.txt")
	items, _ := ListTrash(root)
	info := filepath.Join(dir, ".strata", "trash", items[0].ID, "info.json")

	for _, forged := range []string{".strata/versions/x", "docs/.strata/planted"} {
		raw, _ := os.ReadFile(info)
		var m map[string]any
		json.Unmarshal(raw, &m)
		m["path"] = forged
		raw, _ = json.Marshal(m)
		os.WriteFile(info, raw, 0o600)
		if _, err := Restore(root, items[0].ID); err == nil {
			t.Errorf("restore into %q allowed", forged)
		}
	}

	raw, _ := os.ReadFile(info)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["path"] = "../../../../tmp/escaped.txt"
	raw, _ = json.Marshal(m)
	os.WriteFile(info, raw, 0o600)
	got, err := Restore(root, items[0].ID)
	if err != nil || got != "tmp/escaped.txt" || read(t, dir, "tmp/escaped.txt") != "payload" {
		t.Errorf("traversal not clamped into the share: %q %v", got, err)
	}
}

func TestPurgeEmptyAndSweep(t *testing.T) {
	root, dir := share(t)
	for _, n := range []string{"a", "b", "c", "d"} {
		write(t, dir, n, n)
		Trash(root, n)
	}
	items, _ := ListTrash(root)
	if err := Purge(root, items[0].ID); err != nil {
		t.Fatal(err)
	}

	// Age one record past the limit and leave an old orphan directory.
	old := items[1]
	info := filepath.Join(dir, ".strata", "trash", old.ID, "info.json")
	raw, _ := os.ReadFile(info)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["deleted"] = time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	raw, _ = json.Marshal(m)
	os.WriteFile(info, raw, 0o600)
	orphan := filepath.Join(dir, ".strata", "trash", "00000000000000000000beef")
	os.MkdirAll(orphan, 0o700)
	os.Chtimes(orphan, time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))

	n, _ := SweepTrash(root, 30*24*time.Hour)
	if n != 2 {
		t.Errorf("swept %d, want 2 (expired + orphan)", n)
	}
	if items, _ := ListTrash(root); len(items) != 2 {
		t.Errorf("after sweep: %d items, want 2", len(items))
	}
	if err := EmptyTrash(root); err != nil {
		t.Fatal(err)
	}
	if items, _ := ListTrash(root); len(items) != 0 {
		t.Errorf("trash not empty: %+v", items)
	}
}

func TestEverySaveKeepsAVersion(t *testing.T) {
	root, dir := share(t)
	save := func(body string) {
		t.Helper()
		if err := Save(root, "book.xlsx", strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	save("v1")
	if vs, _ := ListVersions(root, "book.xlsx"); len(vs) != 0 {
		t.Fatalf("new file has versions: %+v", vs)
	}
	save("v2")
	save("v3")
	vs, err := ListVersions(root, "book.xlsx")
	if err != nil || len(vs) != 2 {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	f, _ := OpenVersion(root, "book.xlsx", vs[0].ID)
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "v2" {
		t.Errorf("newest version = %q, want v2", b)
	}

	// Restoring is itself undoable: the replaced content becomes a version.
	if err := RestoreVersion(root, "book.xlsx", vs[1].ID); err != nil {
		t.Fatal(err)
	}
	if read(t, dir, "book.xlsx") != "v1" {
		t.Errorf("restore gave %q", read(t, dir, "book.xlsx"))
	}
	if vs, _ := ListVersions(root, "book.xlsx"); len(vs) != 3 {
		t.Errorf("after restore: %d versions, want 3", len(vs))
	}
}

func TestVersionRetentionAndRefusals(t *testing.T) {
	root, _ := share(t)
	for i := 0; i < KeepVersions+6; i++ {
		Save(root, "f.txt", strings.NewReader(strings.Repeat("x", i)))
	}
	if vs, _ := ListVersions(root, "f.txt"); len(vs) != KeepVersions {
		t.Errorf("kept %d versions, want %d", len(vs), KeepVersions)
	}
	if _, err := OpenVersion(root, "f.txt", "../../../etc/passwd"); err == nil {
		t.Error("version id traversal accepted")
	}
	if _, err := ListVersions(root, ".strata/x"); !errors.Is(err, safepath.ErrReserved) {
		t.Errorf("versions of reserved path: %v", err)
	}
	if vs, err := ListVersions(root, "never-saved.txt"); err != nil || len(vs) != 0 {
		t.Errorf("unsaved file: %v %v", vs, err)
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/mnt/My\040Data/x`); got != "/mnt/My Data/x" {
		t.Errorf("got %q", got)
	}
}
