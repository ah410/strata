//go:build linux || darwin

package fileops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// ExtractLimits bound what one archive may write. Archives are untrusted:
// a 40 KB zip can claim to inflate to petabytes.
type ExtractLimits struct {
	MaxFiles int
	MaxBytes int64
}

// archiveFormat names the format Strata can unpack from rel's name, or "".
func archiveFormat(rel string) string {
	n := strings.ToLower(rel)
	switch {
	case strings.HasSuffix(n, ".zip"):
		return "zip"
	case strings.HasSuffix(n, ".tar.gz"), strings.HasSuffix(n, ".tgz"):
		return "tgz"
	case strings.HasSuffix(n, ".tar"):
		return "tar"
	}
	return ""
}

// archiveStem is the archive's name without its archive extension.
func archiveStem(name string) string {
	n := strings.ToLower(name)
	for _, ext := range []string{".tar.gz", ".tgz", ".zip", ".tar"} {
		if strings.HasSuffix(n, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

// ExtractCheck vets an extract request before a job is started for it.
func ExtractCheck(root *safepath.Root, rel string) (string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return "", err
	}
	if archiveFormat(rel) == "" {
		return "", userr.Error("Strata can extract .zip, .tar, .tar.gz and .tgz archives")
	}
	fi, err := root.FS().Lstat(rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	if !fi.Mode().IsRegular() {
		return "", userr.Error("not a file")
	}
	return rel, nil
}

// Extract unpacks the archive at rel into a new folder beside it, named
// after the archive, and returns that folder. Nothing existing is ever
// overwritten: entries are written into a hidden staging folder that is
// renamed into place only once everything succeeded.
func Extract(ctx context.Context, root *safepath.Root, rel string, lim ExtractLimits, progress func(done, total int64)) (string, error) {
	rel, err := ExtractCheck(root, rel)
	if err != nil {
		return "", err
	}
	dir := path.Dir(rel)
	stage, err := createStage(root, dir)
	if err != nil {
		return "", safepath.Err(err)
	}
	defer root.FS().RemoveAll(stage) // no-op once renamed

	x := &extractor{ctx: ctx, root: root, stage: stage, lim: lim, progress: progress}
	f, err := root.FS().Open(rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	switch archiveFormat(rel) {
	case "zip":
		err = x.zip(f, fi.Size())
	case "tgz":
		cr := &countReader{r: f}
		x.read, x.total = &cr.n, fi.Size()
		var gz *gzip.Reader
		if gz, err = gzip.NewReader(cr); err == nil {
			err = x.tar(gz)
		}
		if err != nil && !errors.As(err, new(userr.Error)) && !errors.Is(err, context.Canceled) {
			err = userr.Error("the archive is damaged or not a gzipped tar")
		}
	case "tar":
		cr := &countReader{r: f}
		x.read, x.total = &cr.n, fi.Size()
		err = x.tar(cr)
	}
	if err != nil {
		return "", err
	}

	stem := archiveStem(path.Base(rel))
	if stem == "" {
		stem = "archive"
	}
	dest, err := renameFree(root, stage, path.Join(dir, stem))
	if err != nil {
		return "", safepath.Err(err)
	}
	return apiPath(dest), nil
}

type extractor struct {
	ctx      context.Context
	root     *safepath.Root
	stage    string
	lim      ExtractLimits
	progress func(done, total int64)

	files   int
	written int64
	// For streams, progress is how much of the archive file was read.
	read  *int64
	total int64
}

func (x *extractor) zip(f io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return userr.Error("the archive is damaged or not a zip")
	}
	// Refuse obvious bombs before writing a byte; the claimed sizes can lie,
	// so writes are counted again below.
	var claimed uint64
	for _, zf := range zr.File {
		claimed += zf.UncompressedSize64
	}
	if len(zr.File) > x.lim.MaxFiles {
		return userr.Error("the archive has too many entries")
	}
	if claimed > uint64(x.lim.MaxBytes) {
		return userr.Error("the archive is larger than the free space")
	}
	x.total = int64(claimed)
	for _, zf := range zr.File {
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			if err := x.dir(zf.Name); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := zf.Open()
			if err != nil {
				return userr.Error("the archive uses a compression method Strata can't read")
			}
			err = x.file(zf.Name, rc, zf.Modified)
			rc.Close()
			if err != nil {
				return err
			}
		}
		// Symlinks and devices are skipped: a link could point anywhere.
	}
	return nil
}

func (x *extractor) tar(r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return userr.Error("the archive is damaged or not a tar")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = x.dir(h.Name)
		case tar.TypeReg:
			err = x.file(h.Name, tr, h.ModTime)
		}
		if err != nil {
			return err
		}
	}
}

// target maps an entry name to its place in the staging folder, or "" for
// entries that are skipped (macOS resource forks).
func (x *extractor) target(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	if strings.HasPrefix(name, "/") || len(name) > 1 && name[1] == ':' {
		return "", userr.Error("the archive contains an absolute path; refusing to extract it")
	}
	parts := strings.Split(strings.Trim(name, "/"), "/")
	for _, p := range parts {
		if p == ".." {
			return "", userr.Error("the archive contains a path that climbs out of its folder; refusing to extract it")
		}
		if p == safepath.Internal || strings.ContainsRune(p, 0) {
			return "", userr.Error("the archive contains a reserved name")
		}
	}
	if parts[0] == "__MACOSX" || parts[len(parts)-1] == ".DS_Store" {
		return "", nil
	}
	clean := path.Clean(strings.Join(parts, "/"))
	if clean == "." || clean == "" {
		return "", nil
	}
	return path.Join(x.stage, clean), nil
}

func (x *extractor) count() error {
	if x.ctx.Err() != nil {
		return x.ctx.Err()
	}
	x.files++
	if x.files > x.lim.MaxFiles {
		return userr.Error("the archive has too many entries")
	}
	return nil
}

func (x *extractor) dir(name string) error {
	if err := x.count(); err != nil {
		return err
	}
	t, err := x.target(name)
	if err != nil || t == "" {
		return err
	}
	return x.root.FS().MkdirAll(t, 0o755)
}

func (x *extractor) file(name string, r io.Reader, mod time.Time) error {
	if err := x.count(); err != nil {
		return err
	}
	t, err := x.target(name)
	if err != nil || t == "" {
		return err
	}
	if err := x.root.FS().MkdirAll(path.Dir(t), 0o755); err != nil {
		return err
	}
	// Final mode at creation: datasets with aclmode=restricted refuse chmod.
	// O_EXCL: an archive naming one path twice fails instead of overwriting.
	f, err := x.root.FS().OpenFile(t, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return userr.Error("the archive lists " + strings.TrimPrefix(t, x.stage+"/") + " twice")
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(f, &limitWriter{x: x, r: r})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if !mod.IsZero() {
		x.root.FS().Chtimes(t, mod, mod)
	}
	return nil
}

// limitWriter counts inflated bytes as they're read, against MaxBytes.
type limitWriter struct {
	x *extractor
	r io.Reader
}

func (l *limitWriter) Read(p []byte) (int, error) {
	if err := l.x.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := l.r.Read(p)
	l.x.written += int64(n)
	if l.x.written > l.x.lim.MaxBytes {
		return n, userr.Error("the archive is larger than the free space")
	}
	if l.x.progress != nil {
		if l.x.read != nil {
			l.x.progress(*l.x.read, l.x.total)
		} else {
			l.x.progress(l.x.written, l.x.total)
		}
	}
	return n, err
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// createStage makes a hidden folder in dir that List and Zip leave out.
func createStage(root *safepath.Root, dir string) (string, error) {
	f, name, err := createTemp(root, dir)
	if err != nil {
		return "", err
	}
	f.Close()
	root.FS().Remove(name)
	return name, root.FS().Mkdir(name, 0o755)
}

// renameFree moves from to p, or "p (2)", "p (3)", ... whichever is free.
// A name taken between the check and the rename just moves on to the next.
func renameFree(root *safepath.Root, from, p string) (string, error) {
	cand := p
	for n := 2; n < 10000; n++ {
		if _, err := root.FS().Lstat(cand); errors.Is(err, fs.ErrNotExist) {
			err := root.FS().Rename(from, cand)
			if !errors.Is(err, fs.ErrExist) {
				return cand, err
			}
		}
		cand = p + " (" + strconv.Itoa(n) + ")"
	}
	return "", userr.Error("too many folders with this name")
}
