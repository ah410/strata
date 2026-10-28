package fileops

import (
	"errors"
	"os"
	"path"

	"strata/internal/safepath"
)

// AtomicFile is a file being written in place of rel. Nothing at rel changes
// until Close, which renames it into place and keeps the old contents as a
// version, the same as every other save.
type AtomicFile struct {
	root    *safepath.Root
	rel     string
	tmpName string
	f       *os.File
	werr    error
	// failed reports, at Close, whether the source of the data broke
	// (a dropped upload), in which case the write is discarded.
	failed func() bool
}

// CreateAtomic starts writing rel. Its folder must already exist.
func CreateAtomic(root *safepath.Root, rel string, failed func() bool) (*AtomicFile, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, os.ErrInvalid
	}
	fi, err := root.FS().Stat(path.Dir(rel))
	if err != nil {
		return nil, safepath.Err(err)
	}
	if !fi.IsDir() {
		return nil, os.ErrNotExist
	}
	if fi, err := root.FS().Lstat(rel); err == nil && fi.IsDir() {
		return nil, os.ErrExist
	}
	f, name, err := createTemp(root, path.Dir(rel))
	if err != nil {
		return nil, safepath.Err(err)
	}
	if failed == nil {
		failed = func() bool { return false }
	}
	return &AtomicFile{root: root, rel: rel, tmpName: name, f: f, failed: failed}, nil
}

func (a *AtomicFile) Write(p []byte) (int, error) {
	n, err := a.f.Write(p)
	if err != nil && a.werr == nil {
		a.werr = err
	}
	return n, err
}

// Stat describes the data written so far.
func (a *AtomicFile) Stat() (os.FileInfo, error) { return a.f.Stat() }

// Close commits the write, or discards it if anything went wrong.
func (a *AtomicFile) Close() error {
	defer a.root.FS().Remove(a.tmpName) // no-op once renamed
	err := a.f.Sync()
	if cerr := a.f.Close(); err == nil {
		err = cerr
	}
	switch {
	case a.werr != nil:
		return a.werr
	case err != nil:
		return err
	case a.failed():
		return errors.New("upload was interrupted; the file was left unchanged")
	}
	return safepath.Err(replace(a.root, a.tmpName, a.rel))
}
