// Package fileops implements directory listing and mutation on top of a
// safepath.Root. Nothing here accepts an absolute path from the caller;
// everything is resolved through the root first.
package fileops

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Entry is one row in a directory listing.
type Entry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"` // root-relative, forward slashes
	IsDir    bool      `json:"isDir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Kind     string    `json:"kind"` // sheet, image, pdf, video, audio, text, archive, file
}

// Listing is a directory plus the crumbs needed to navigate back up.
type Listing struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

// List returns the contents of dir, directories first then files, each group
// sorted case-insensitively by name.
func List(root *safepath.Root, rel string) (*Listing, error) {
	rel = safepath.Clean(rel)
	d, err := root.FS().Open(rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	defer d.Close()

	info, err := d.Stat()
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, userr.Error("not a directory")
	}

	dirents, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(dirents))
	for _, de := range dirents {
		name := de.Name()
		// Hide dotfiles; on a NAS these are overwhelmingly .DS_Store,
		// @eaDir, and other noise the user did not put there.
		if strings.HasPrefix(name, ".") {
			continue
		}

		p := apiPath(path.Join(rel, name))
		// Stat through the root, not de.Info(), so the lookup is bounded too.
		fi, err := root.FS().Lstat(path.Join(rel, name))
		if err != nil {
			// A file can vanish between ReadDir and Lstat. Skip it rather
			// than failing the whole listing.
			continue
		}

		entries = append(entries, Entry{
			Name:     name,
			Path:     p,
			IsDir:    fi.IsDir(),
			Size:     fi.Size(),
			Modified: fi.ModTime(),
			Kind:     classify(name, fi.IsDir()),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return &Listing{Path: apiPath(rel), Entries: entries}, nil
}

// apiPath is the form the UI speaks: "" for the root, no leading slash.
func apiPath(rel string) string {
	if rel == "." {
		return ""
	}
	return rel
}

// classify buckets a filename into a coarse kind the UI uses to pick an icon
// and decide whether an inline preview is possible.
func classify(name string, isDir bool) string {
	if isDir {
		return "folder"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx", ".xlsm", ".xltx":
		return "sheet"
	case ".csv", ".tsv":
		return "csv"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".avif":
		return "image"
	case ".pdf":
		return "pdf"
	case ".mp4", ".mkv", ".mov", ".webm", ".avi":
		return "video"
	case ".mp3", ".flac", ".wav", ".m4a", ".ogg", ".opus":
		return "audio"
	case ".txt", ".md", ".log", ".json", ".yaml", ".yml", ".toml", ".ini",
		".conf", ".sh", ".go", ".js", ".ts", ".tsx", ".jsx", ".py", ".java",
		".kt", ".rs", ".c", ".h", ".cpp", ".sql", ".html", ".css", ".xml", ".env":
		return "text"
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar":
		return "archive"
	case ".docx", ".doc", ".odt":
		return "doc"
	case ".pptx", ".ppt":
		return "slides"
	}
	return "file"
}

// Mkdir creates a directory, including any missing parents.
func Mkdir(root *safepath.Root, rel string) error {
	rel = safepath.Clean(rel)
	if _, err := root.FS().Lstat(rel); err == nil {
		return userr.Error("already exists")
	}
	return safepath.Err(root.FS().MkdirAll(rel, 0o755))
}

// Move renames or relocates a path. Both ends are bounded by the root, so a
// move cannot be used to write outside the share.
func Move(root *safepath.Root, fromRel, toRel string) error {
	from, to := safepath.Clean(fromRel), safepath.Clean(toRel)
	if from == "." || to == "." {
		return userr.Error("cannot move the share root")
	}
	if _, err := root.FS().Lstat(to); err == nil {
		return userr.Error("destination already exists")
	}
	if err := root.FS().MkdirAll(path.Dir(to), 0o755); err != nil {
		return safepath.Err(err)
	}
	err := root.FS().Rename(from, to)
	// Rename fails across filesystems, which happens when the user moves a
	// file between nested datasets. Only then fall back to copy+remove.
	if errors.Is(err, syscall.EXDEV) {
		if err := copyTree(root, from, to); err != nil {
			return safepath.Err(err)
		}
		return safepath.Err(root.FS().RemoveAll(from))
	}
	return safepath.Err(err)
}

// Copy duplicates a file or directory tree.
func Copy(root *safepath.Root, fromRel, toRel string) error {
	from, to := safepath.Clean(fromRel), safepath.Clean(toRel)
	if to == "." {
		return userr.Error("destination already exists")
	}
	info, err := root.FS().Lstat(from)
	if err != nil {
		return safepath.Err(err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return userr.Error("only files and folders can be copied")
	}
	if _, err := root.FS().Lstat(to); err == nil {
		return userr.Error("destination already exists")
	}
	if err := root.FS().MkdirAll(path.Dir(to), 0o755); err != nil {
		return safepath.Err(err)
	}
	return safepath.Err(copyTree(root, from, to))
}

// copyTree copies regular files and directories. Symlinks are skipped, never
// followed: a link inside the share must not pull outside content in.
func copyTree(root *safepath.Root, from, to string) error {
	info, err := root.FS().Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.Mode().IsRegular():
		return copyFile(root, from, to, info.Mode().Perm())
	case !info.IsDir():
		return nil // symlink, device, socket: not ours to copy
	}
	if err := root.FS().MkdirAll(to, info.Mode().Perm()|0o700); err != nil {
		return err
	}
	d, err := root.FS().Open(from)
	if err != nil {
		return err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := copyTree(root, path.Join(from, n), path.Join(to, n)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(root *safepath.Root, from, to string, mode os.FileMode) error {
	src, err := root.FS().Open(from)
	if err != nil {
		return err
	}
	defer src.Close()

	// O_EXCL: never write through something that appeared at the target.
	dst, err := root.FS().OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// Delete removes a file or directory tree. RemoveAll does not follow
// symlinks, so deleting a link removes the link only.
func Delete(root *safepath.Root, rel string) error {
	rel = safepath.Clean(rel)
	if rel == "." {
		return userr.Error("refusing to delete root")
	}
	return safepath.Err(root.FS().RemoveAll(rel))
}

// Save writes r to rel, creating parent directories as needed. An existing
// file is replaced atomically via a temp file in the same directory, so a
// failed or interrupted write never truncates the original.
func Save(root *safepath.Root, rel string, r io.Reader) error {
	return WriteAtomic(root, rel, func(w io.Writer) error {
		_, err := io.Copy(w, r)
		return err
	})
}

// WriteAtomic creates rel's contents through write, then renames the result
// into place. Readers see either the old file or the complete new one.
func WriteAtomic(root *safepath.Root, rel string, write func(io.Writer) error) error {
	rel = safepath.Clean(rel)
	if rel == "." {
		return userr.Error("invalid file name")
	}
	dir := path.Dir(rel)
	if err := root.FS().MkdirAll(dir, 0o755); err != nil {
		return safepath.Err(err)
	}

	tmp, tmpName, err := createTemp(root, dir)
	if err != nil {
		return safepath.Err(err)
	}
	defer root.FS().Remove(tmpName) // no-op once the rename succeeds

	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return safepath.Err(root.FS().Rename(tmpName, rel))
}

// createTemp opens a new hidden temp file in dir with its final mode, so no
// chmod is needed later (ZFS aclmode=restricted rejects chmod on ACL files).
func createTemp(root *safepath.Root, dir string) (*os.File, string, error) {
	for range 10 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, "", err
		}
		name := path.Join(dir, ".strata-"+hex.EncodeToString(b[:]))
		f, err := root.FS().OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return f, name, err
	}
	return nil, "", fmt.Errorf("createTemp: no unique name in %s", dir)
}

// DiskUsage reports the total and free bytes of the filesystem holding root.
type DiskUsage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
	Used  uint64 `json:"used"`
}
