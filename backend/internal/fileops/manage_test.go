package fileops

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSearchFindsNamesAndSkipsHidden(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "Reports/Q1 Budget.xlsx", "x")
	write(t, dir, "Reports/old/budget-2019.csv", "x")
	write(t, dir, "notes.txt", "budget inside, not in name")
	write(t, dir, ".strata/trash/abc/item/budget.xlsx", "deleted")
	write(t, dir, ".hidden/budget.txt", "x")
	outside := t.TempDir()
	write(t, outside, "budget-secret.txt", "x")
	os.Symlink(outside, filepath.Join(dir, "link"))

	res, err := Search(context.Background(), root, "", "BUDGET")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range res.Entries {
		got = append(got, e.Path)
	}
	sort.Strings(got)
	want := []string{"Reports/Q1 Budget.xlsx", "Reports/old/budget-2019.csv"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v, want %v", got, want)
	}

	if res, _ := Search(context.Background(), root, "Reports/old", "budget"); len(res.Entries) != 1 {
		t.Errorf("scoped search: %+v", res.Entries)
	}
	for _, bad := range []struct{ dir, q string }{{"", "b"}, {".strata", "budget"}, {"link", "budget"}, {"../..", "x"}} {
		if res, err := Search(context.Background(), root, bad.dir, bad.q); err == nil && len(res.Entries) > 0 {
			t.Errorf("Search(%q,%q) returned %v", bad.dir, bad.q, res.Entries)
		}
	}
}

func TestSearchStopsAtLimits(t *testing.T) {
	root, dir := share(t)
	for i := range SearchMaxResults + 10 {
		write(t, dir, filepath.Join("many", strings.Repeat("a", 1+i%5)+"-match-"+time.Duration(i).String()), "")
	}
	res, err := Search(context.Background(), root, "", "match")
	if err != nil || len(res.Entries) != SearchMaxResults || !res.Truncated {
		t.Fatalf("got %d entries, truncated=%v, err=%v", len(res.Entries), res.Truncated, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res, _ := Search(ctx, root, "", "match"); !res.Truncated {
		t.Error("cancelled search not marked truncated")
	}
}

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(body)
	}
	return out
}

func TestZipArchivesSelection(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "a/b/photo.jpg", "jpeg")
	write(t, dir, "a/b/doc.txt", "text")
	write(t, dir, "a/b/.strata/trash/x/item", "deleted")
	write(t, dir, "a/b/.strata-0123", "temp")
	write(t, dir, "other/doc.txt", "second doc")
	outside := t.TempDir()
	write(t, outside, "secret", "s")
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "a/b/escape"))

	paths, err := ZipCheck(root, []string{"a/b", "other/doc.txt", "a/b/doc.txt"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Zip(context.Background(), root, paths, &buf); err != nil {
		t.Fatal(err)
	}
	got := unzip(t, buf.Bytes())
	want := map[string]string{"b/": "", "b/photo.jpg": "jpeg", "b/doc.txt": "text", "doc.txt": "second doc", "doc (2).txt": "text"}
	if len(got) != len(want) {
		t.Fatalf("entries %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}

	for _, bad := range [][]string{nil, {".strata"}, {"a/b/.strata/trash"}, {"a/b/escape"}, {"missing"}} {
		if _, err := ZipCheck(root, bad); err == nil {
			t.Errorf("ZipCheck(%v) accepted", bad)
		}
	}
}

func TestResumableUpload(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "media/movie.mkv", "old cut")
	body := strings.Repeat("0123456789", 1000)

	id, err := StartUpload(root, "media/movie.mkv", int64(len(body)), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteChunk(root, id, 0, strings.NewReader(body[:4000])); err != nil {
		t.Fatal(err)
	}
	// A retried or reordered chunk is refused with the true offset.
	var oe *OffsetError
	if _, err := WriteChunk(root, id, 0, strings.NewReader(body[:4000])); !errors.As(err, &oe) || oe.Offset != 4000 {
		t.Fatalf("stale chunk: %v", err)
	}
	if _, err := FinishUpload(root, id); err == nil {
		t.Fatal("incomplete upload finished")
	}
	off, size, _ := UploadOffset(root, id)
	if off != 4000 || size != int64(len(body)) {
		t.Fatalf("offset %d/%d", off, size)
	}
	// Bytes past the declared size are refused and rolled back.
	if _, err := WriteChunk(root, id, 4000, strings.NewReader(body[4000:]+"extra")); err == nil {
		t.Fatal("oversized chunk accepted")
	}
	if off, _, _ := UploadOffset(root, id); off != 4000 {
		t.Fatalf("rollback left offset %d", off)
	}
	if _, err := WriteChunk(root, id, 4000, strings.NewReader(body[4000:])); err != nil {
		t.Fatal(err)
	}
	p, err := FinishUpload(root, id)
	if err != nil || p != "media/movie.mkv" || read(t, dir, p) != body {
		t.Fatalf("finish: %q %v", p, err)
	}
	if vs, _ := ListVersions(root, "media/movie.mkv"); len(vs) != 1 {
		t.Errorf("replaced file not versioned: %v", vs)
	}
	if _, _, err := UploadOffset(root, id); err == nil {
		t.Error("finished upload still resumable")
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, ".strata/uploads")); len(ents) != 0 {
		t.Errorf("leftovers: %v", ents)
	}
}

func TestUploadRefusalsAndSweep(t *testing.T) {
	root, dir := share(t)
	for _, bad := range []string{"", ".strata/x", "a/.strata/x"} {
		if _, err := StartUpload(root, bad, 1, 10); err == nil {
			t.Errorf("StartUpload(%q) accepted", bad)
		}
	}
	// ".." is clamped at the root, never followed out of it.
	clamped, err := StartUpload(root, "../../etc/x", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "etc")); err == nil {
		t.Fatal("upload escaped the root")
	}
	AbortUpload(root, clamped)
	if _, err := StartUpload(root, "big", 11, 10); err == nil {
		t.Error("over-limit size accepted")
	}
	if _, err := WriteChunk(root, "../../../etc/passwd", 0, strings.NewReader("x")); err == nil {
		t.Error("bad id accepted")
	}

	id, _ := StartUpload(root, "stale.bin", 5, 10)
	old := time.Now().Add(-48 * time.Hour)
	for _, ext := range []string{".part", ".json"} {
		os.Chtimes(filepath.Join(dir, ".strata/uploads", id+ext), old, old)
	}
	fresh, _ := StartUpload(root, "fresh.bin", 5, 10)
	if n := SweepUploads(root, UploadMaxAge); n != 1 {
		t.Errorf("swept %d, want 1", n)
	}
	if _, _, err := UploadOffset(root, fresh); err != nil {
		t.Error("fresh upload swept")
	}
	if err := AbortUpload(root, fresh); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(filepath.Join(dir, ".strata/uploads")); len(ents) != 0 {
		t.Errorf("leftovers: %v", ents)
	}
}

func TestNoMoveOrCopyIntoItself(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "a/sub/f.txt", "f")
	write(t, dir, "ab/g.txt", "g")
	for _, to := range []string{"a/sub/a", "a"} {
		if err := Copy(root, "a", to); err == nil {
			t.Errorf("Copy(a, %s) accepted", to)
		}
		if err := Move(root, "a", to); err == nil {
			t.Errorf("Move(a, %s) accepted", to)
		}
	}
	// A sibling sharing the prefix is not "inside".
	if err := Copy(root, "a", "ab/a"); err != nil {
		t.Fatal(err)
	}
}

func TestTextSaveDetectsConflicts(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "notes.md", "# v1")
	v1, err := ReadText(root, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := WriteText(root, "notes.md", v1.Version, "# v2")
	if err != nil || read(t, dir, "notes.md") != "# v2" {
		t.Fatalf("save: %v", err)
	}
	// Someone else saved meanwhile (another tab, an SMB client).
	write(t, dir, "notes.md", "# theirs")
	if _, err := WriteText(root, "notes.md", v2, "# mine"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if read(t, dir, "notes.md") != "# theirs" {
		t.Fatal("stale save overwrote")
	}
	if vs, _ := ListVersions(root, "notes.md"); len(vs) != 1 {
		t.Errorf("save not versioned: %v", vs)
	}
	// Empty version creates, and never replaces.
	if _, err := WriteText(root, "new.txt", "", "hi"); err != nil || read(t, dir, "new.txt") != "hi" {
		t.Fatalf("create: %v", err)
	}
	if _, err := WriteText(root, "new.txt", "", "clobber"); !errors.Is(err, ErrConflict) {
		t.Fatalf("create over existing: %v", err)
	}
	write(t, dir, "big.log", strings.Repeat("x", TextMax+10))
	big, _ := ReadText(root, "big.log")
	if !big.Truncated || big.Version != "" {
		t.Fatal("truncated file is editable")
	}
	if _, err := WriteText(root, "big.log", "", "x"); err == nil {
		t.Fatal("truncated file overwritten")
	}
	for _, bad := range []string{".strata/x.txt", "a/.strata-temp", "."} {
		if _, err := WriteText(root, bad, "", "x"); err == nil {
			t.Errorf("WriteText(%q) accepted", bad)
		}
	}
}
