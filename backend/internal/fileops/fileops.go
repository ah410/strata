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
	abs, err := root.Resolve(rel)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, userr.Error("not a directory")
	}

	dirents, err := os.ReadDir(abs)
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

		fi, err := de.Info()
		if err != nil {
			// A file can vanish between ReadDir and Info. Skip it rather
			// than failing the whole listing.
			continue
		}

		entries = append(entries, Entry{
			Name:     name,
			Path:     path.Join(rel, name),
			IsDir:    de.IsDir(),
			Size:     fi.Size(),
			Modified: fi.ModTime(),
			Kind:     classify(name, de.IsDir()),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return &Listing{Path: rel, Entries: entries}, nil
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
	abs, err := root.Resolve(rel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return userr.Error("already exists")
	}
	return os.MkdirAll(abs, 0o755)
}

// Move renames or relocates a path. Both ends are resolved through the root,
// so a move cannot be used to write outside the share.
func Move(root *safepath.Root, fromRel, toRel string) error {
	from, err := root.Resolve(fromRel)
	if err != nil {
		return err
	}
	to, err := root.Resolve(toRel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(to); err == nil {
		return userr.Error("destination already exists")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	// Rename fails across filesystems, which happens when the user drags a
	// file between two differently-mounted datasets. Fall back to copy+remove.
	if err := os.Rename(from, to); err != nil {
		if copyErr := copyTree(from, to); copyErr != nil {
			return copyErr
		}
		return os.RemoveAll(from)
	}
	return nil
}

// Copy duplicates a file or directory tree.
func Copy(root *safepath.Root, fromRel, toRel string) error {
	from, err := root.Resolve(fromRel)
	if err != nil {
		return err
	}
	to, err := root.Resolve(toRel)
	if err != nil {
		return err
	}
	if _, err := os.Stat(to); err == nil {
		return userr.Error("destination already exists")
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return copyTree(from, to)
}

func copyTree(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(from, to, info.Mode())
	}
	if err := os.MkdirAll(to, info.Mode()); err != nil {
		return err
	}
	ents, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := copyTree(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// Delete removes a file or directory tree.
func Delete(root *safepath.Root, rel string) error {
	abs, err := root.Resolve(rel)
	if err != nil {
		return err
	}
	// Refuse to delete the share root itself.
	if abs == root.Path() {
		return userr.Error("refusing to delete root")
	}
	return os.RemoveAll(abs)
}

// Save writes r to rel, creating parent directories as needed. An existing
// file is replaced atomically via a temp file in the same directory, so a
// failed or interrupted write never truncates the original.
func Save(root *safepath.Root, rel string, r io.Reader) error {
	abs, err := root.Resolve(rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := createTemp(dir)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := io.Copy(tmp, r); err != nil {
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
	return os.Rename(tmpName, abs)
}

// createTemp opens a new hidden temp file in dir with its final mode, so no
// chmod is needed later (ZFS aclmode=restricted rejects chmod on ACL files).
func createTemp(dir string) (*os.File, error) {
	for range 10 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		name := filepath.Join(dir, ".strata-"+hex.EncodeToString(b[:]))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, fmt.Errorf("createTemp: no unique name in %s", dir)
}

// DiskUsage reports the total and free bytes of the filesystem holding root.
type DiskUsage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
	Used  uint64 `json:"used"`
}
