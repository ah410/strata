package fileops

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var roomy = ExtractLimits{MaxFiles: 1000, MaxBytes: 1 << 30}

type entry struct {
	name, body string
	mode       fs.FileMode
}

func makeZip(t *testing.T, dir, rel string, entries ...entry) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	write(t, dir, rel, buf.String())
}

func makeTgz(t *testing.T, dir, rel string, entries ...tar.Header) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := h.Linkname
		if h.Typeflag == tar.TypeReg {
			h.Linkname = ""
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(body))
		}
	}
	tw.Close()
	gz.Close()
	write(t, dir, rel, buf.String())
}

func TestExtractZipIntoNewFolder(t *testing.T) {
	root, dir := share(t)
	makeZip(t, dir, "in/Photos.zip",
		entry{name: "Photos/"},
		entry{name: "Photos/a.txt", body: "alpha"},
		entry{name: "Photos/sub/b.txt", body: "beta"},
		entry{name: "__MACOSX/._a.txt", body: "junk"},
	)
	var last int64
	dest, err := Extract(context.Background(), root, "in/Photos.zip", roomy, func(d, _ int64) { last = d })
	if err != nil {
		t.Fatal(err)
	}
	if dest != "in/Photos" {
		t.Fatalf("dest = %q", dest)
	}
	if read(t, dir, "in/Photos/Photos/a.txt") != "alpha" || read(t, dir, "in/Photos/Photos/sub/b.txt") != "beta" {
		t.Fatal("contents wrong")
	}
	if _, err := os.Stat(filepath.Join(dir, "in/Photos/__MACOSX")); err == nil {
		t.Fatal("__MACOSX was extracted")
	}
	if last == 0 {
		t.Fatal("no progress reported")
	}
	// Nothing hidden left behind.
	ents, _ := os.ReadDir(filepath.Join(dir, "in"))
	if len(ents) != 2 {
		t.Fatalf("leftovers in folder: %v", ents)
	}
}

func TestExtractNeverOverwrites(t *testing.T) {
	root, dir := share(t)
	makeZip(t, dir, "a.zip", entry{name: "x.txt", body: "new"})
	write(t, dir, "a/x.txt", "mine")
	dest, err := Extract(context.Background(), root, "a.zip", roomy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "a (2)" || read(t, dir, "a/x.txt") != "mine" || read(t, dir, "a (2)/x.txt") != "new" {
		t.Fatalf("dest = %q", dest)
	}
}

func TestExtractRefusesZipSlip(t *testing.T) {
	for _, name := range []string{"../evil.txt", "a/../../evil.txt", "/etc/evil", `..\evil.txt`, "C:/evil", ".strata/trash/x"} {
		root, dir := share(t)
		makeZip(t, dir, "bad.zip", entry{name: "ok.txt", body: "ok"}, entry{name: name, body: "pwn"})
		if _, err := Extract(context.Background(), root, "bad.zip", roomy, nil); err == nil {
			t.Errorf("%q: extract succeeded", name)
		}
		ents, _ := os.ReadDir(dir)
		if len(ents) != 1 {
			t.Errorf("%q: left files behind: %v", name, ents)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
			t.Fatalf("%q escaped the share", name)
		}
	}
}

func TestExtractSkipsSymlinks(t *testing.T) {
	root, dir := share(t)
	makeZip(t, dir, "l.zip", entry{name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}, entry{name: "f.txt", body: "f"})
	makeTgz(t, dir, "l.tgz",
		tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"},
		tar.Header{Name: "f.txt", Typeflag: tar.TypeReg, Linkname: "f", Mode: 0o644},
	)
	for _, a := range []string{"l.zip", "l.tgz"} {
		dest, err := Extract(context.Background(), root, a, roomy, nil)
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, dest, "link")); err == nil {
			t.Errorf("%s: symlink extracted", a)
		}
		if _, err := os.Lstat(filepath.Join(dir, dest, "hard")); err == nil {
			t.Errorf("%s: hardlink extracted", a)
		}
		if read(t, dir, dest+"/f.txt") != "f" {
			t.Errorf("%s: regular file missing", a)
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	root, dir := share(t)
	makeTgz(t, dir, "backup.tar.gz",
		tar.Header{Name: "./docs/", Typeflag: tar.TypeDir, Mode: 0o755},
		tar.Header{Name: "./docs/r.md", Typeflag: tar.TypeReg, Linkname: "# hi", Mode: 0o600},
	)
	dest, err := Extract(context.Background(), root, "backup.tar.gz", roomy, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "backup" || read(t, dir, "backup/docs/r.md") != "# hi" {
		t.Fatalf("dest = %q", dest)
	}
}

func TestExtractBombLimits(t *testing.T) {
	root, dir := share(t)
	big := strings.Repeat("0", 1<<20)
	makeZip(t, dir, "bomb.zip", entry{name: "a", body: big}, entry{name: "b", body: big})
	_, err := Extract(context.Background(), root, "bomb.zip", ExtractLimits{MaxFiles: 10, MaxBytes: 1 << 20}, nil)
	if err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("zip over the byte limit: %v", err)
	}
	// A tar stream has no directory up front, so the limit applies as it writes.
	makeTgz(t, dir, "bomb.tgz", tar.Header{Name: "a", Typeflag: tar.TypeReg, Linkname: big + big, Mode: 0o644})
	_, err = Extract(context.Background(), root, "bomb.tgz", ExtractLimits{MaxFiles: 10, MaxBytes: 1 << 20}, nil)
	if err == nil || !strings.Contains(err.Error(), "free space") {
		t.Fatalf("tgz over the byte limit: %v", err)
	}
	makeZip(t, dir, "many.zip", entry{name: "1"}, entry{name: "2"}, entry{name: "3"})
	if _, err := Extract(context.Background(), root, "many.zip", ExtractLimits{MaxFiles: 2, MaxBytes: 1 << 20}, nil); err == nil {
		t.Fatal("too many entries accepted")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 3 {
		t.Fatalf("failed extracts left files: %v", ents)
	}
}

func TestExtractDuplicateEntryFails(t *testing.T) {
	root, dir := share(t)
	makeZip(t, dir, "dup.zip", entry{name: "x", body: "1"}, entry{name: "x", body: "2"})
	if _, err := Extract(context.Background(), root, "dup.zip", roomy, nil); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractCanceled(t *testing.T) {
	root, dir := share(t)
	makeZip(t, dir, "c.zip", entry{name: "x", body: "1"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Extract(ctx, root, "c.zip", roomy, nil); err == nil {
		t.Fatal("canceled extract succeeded")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftovers: %v", ents)
	}
}

func TestExtractCheck(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "x.7z", "x")
	write(t, dir, "d.zip/inner", "x")
	for _, p := range []string{"x.7z", "d.zip", "missing.zip", ".strata/a.zip"} {
		if _, err := ExtractCheck(root, p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	if _, err := Extract(context.Background(), root, "x.7z", roomy, nil); err == nil {
		t.Fatal("unsupported format extracted")
	}
	write(t, dir, "junk.zip", "not a zip")
	if _, err := Extract(context.Background(), root, "junk.zip", roomy, nil); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged zip: %v", err)
	}
}
