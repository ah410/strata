//go:build linux || darwin

package fileops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sync"
	"syscall"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Resumable uploads: the client starts one, PUTs chunks at the offset the
// server reports, and finishes it. Partial data lives in the destination
// dataset's .strata/uploads, so finishing is a rename.
const (
	ChunkMax     = 16 << 20
	UploadMaxAge = 24 * time.Hour
)

// Upload is a partial file and where it will go.
type Upload struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`

	store string
}

// OffsetError means a chunk didn't start where the partial file ends.
type OffsetError struct{ Offset int64 }

func (e *OffsetError) Error() string { return fmt.Sprintf("upload is at offset %d", e.Offset) }

var uploadID = trashID

var (
	lockMu sync.Mutex
	locks  = map[string]*sync.Mutex{}
)

// lockUpload serialises work on one upload; chunks of others run in parallel.
func lockUpload(id string) func() {
	lockMu.Lock()
	m := locks[id]
	if m == nil {
		m = &sync.Mutex{}
		locks[id] = m
	}
	lockMu.Unlock()
	m.Lock()
	return m.Unlock
}

func forgetLock(id string) {
	lockMu.Lock()
	delete(locks, id)
	lockMu.Unlock()
}

func (u Upload) dir() string  { return internalDir(u.store, "uploads") }
func (u Upload) part() string { return path.Join(u.dir(), u.ID+".part") }
func (u Upload) meta() string { return path.Join(u.dir(), u.ID+".json") }

// StartUpload reserves a partial file for rel, which will hold size bytes.
func StartUpload(root *safepath.Root, rel string, size, max int64) (string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return "", err
	}
	if rel == "." || path.Base(rel) == ".." {
		return "", userr.Error("invalid file name")
	}
	if size < 0 || size > max {
		return "", userr.Error("file is too large")
	}
	if err := root.FS().MkdirAll(path.Dir(rel), 0o755); err != nil {
		return "", safepath.Err(err)
	}
	store, err := storeFor(root, rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	id, err := newTrashID()
	if err != nil {
		return "", err
	}
	u := Upload{ID: id, Path: rel, Size: size, Created: time.Now().UTC(), store: store}
	if err := root.FS().MkdirAll(u.dir(), 0o700); err != nil {
		return "", safepath.Err(err)
	}
	raw, _ := json.Marshal(u)
	if err := root.FS().WriteFile(u.meta(), raw, 0o600); err != nil {
		return "", safepath.Err(err)
	}
	f, err := root.FS().OpenFile(u.part(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		root.FS().Remove(u.meta())
		return "", safepath.Err(err)
	}
	f.Close()
	return id, nil
}

func findUpload(root *safepath.Root, id string) (Upload, error) {
	if !uploadID.MatchString(id) {
		return Upload{}, os.ErrNotExist
	}
	for _, s := range stores(root) {
		raw, err := root.FS().ReadFile(path.Join(internalDir(s, "uploads"), id+".json"))
		if err != nil {
			continue
		}
		var u Upload
		if json.Unmarshal(raw, &u) != nil || u.ID != id {
			continue
		}
		u.store = s
		return u, nil
	}
	return Upload{}, os.ErrNotExist
}

// UploadOffset reports how many bytes have arrived so far.
func UploadOffset(root *safepath.Root, id string) (int64, int64, error) {
	u, err := findUpload(root, id)
	if err != nil {
		return 0, 0, err
	}
	fi, err := root.FS().Lstat(u.part())
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), u.Size, nil
}

// WriteChunk appends r at offset, which must be where the file ends now.
// It returns the new offset. Data past the declared size is refused.
func WriteChunk(root *safepath.Root, id string, offset int64, r io.Reader) (int64, error) {
	defer lockUpload(id)()
	u, err := findUpload(root, id)
	if err != nil {
		return 0, err
	}
	f, err := root.FS().OpenFile(u.part(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	cur := fi.Size()
	if offset != cur {
		return cur, &OffsetError{cur}
	}
	n, err := io.Copy(f, io.LimitReader(r, u.Size-cur))
	if err == nil {
		var extra [1]byte
		if m, _ := r.Read(extra[:]); m > 0 {
			err = userr.Error("chunk runs past the declared file size")
		}
	}
	if err != nil {
		// Keep only whole, acknowledged chunks.
		f.Truncate(cur)
		return cur, err
	}
	if err := f.Sync(); err != nil {
		f.Truncate(cur)
		return cur, err
	}
	return cur + n, nil
}

// FinishUpload moves a complete upload into place, keeping any file it
// replaces as a version. It returns the final path.
func FinishUpload(root *safepath.Root, id string) (string, error) {
	defer lockUpload(id)()
	u, err := findUpload(root, id)
	if err != nil {
		return "", err
	}
	fi, err := root.FS().Lstat(u.part())
	if err != nil {
		return "", err
	}
	if fi.Size() != u.Size {
		return "", userr.Error("upload is incomplete")
	}
	// The record is data from disk; vet it like a request.
	rel, err := safepath.User(u.Path)
	if err != nil || rel == "." {
		return "", userr.Error("invalid upload destination")
	}
	if err := root.FS().MkdirAll(path.Dir(rel), 0o755); err != nil {
		return "", safepath.Err(err)
	}
	err = replace(root, u.part(), rel)
	if errors.Is(err, syscall.EXDEV) {
		// The destination moved to another dataset since the upload began.
		var src *os.File
		if src, err = root.FS().Open(u.part()); err == nil {
			err = Save(root, rel, src)
			src.Close()
		}
	}
	if err != nil {
		return "", safepath.Err(err)
	}
	removeUpload(root, u)
	return rel, nil
}

// AbortUpload discards a partial upload.
func AbortUpload(root *safepath.Root, id string) error {
	defer lockUpload(id)()
	u, err := findUpload(root, id)
	if err != nil {
		return err
	}
	removeUpload(root, u)
	return nil
}

func removeUpload(root *safepath.Root, u Upload) {
	root.FS().Remove(u.part())
	root.FS().Remove(u.meta())
	forgetLock(u.ID)
}

// SweepUploads removes uploads whose partial file is untouched for longer
// than maxAge, judged by the part's mtime so a slow upload survives.
func SweepUploads(root *safepath.Root, maxAge time.Duration) int {
	n := 0
	now := time.Now()
	for _, s := range stores(root) {
		dir := internalDir(s, "uploads")
		d, err := root.FS().Open(dir)
		if err != nil {
			continue
		}
		names, _ := d.Readdirnames(-1)
		d.Close()
		for _, name := range names {
			m := uploadFile.FindStringSubmatch(name)
			if m == nil || m[2] != "json" {
				continue
			}
			u := Upload{ID: m[1], store: s}
			fi, err := root.FS().Lstat(u.part())
			if err != nil {
				fi, err = root.FS().Lstat(u.meta())
			}
			if err == nil && now.Sub(fi.ModTime()) > maxAge {
				removeUpload(root, u)
				n++
			}
		}
	}
	return n
}

var uploadFile = regexp.MustCompile(`^([0-9a-f]{24})\.(part|json)$`)
