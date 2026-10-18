//go:build linux || darwin

package fileops

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// TrashItem is one deleted file or folder awaiting restore or purge.
type TrashItem struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Deleted time.Time `json:"deleted"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"isDir"`

	store string
}

// Layout: <store>/.strata/trash/<id>/info.json and .../<id>/item.
var trashID = regexp.MustCompile(`^[0-9a-f]{24}$`)

func newTrashID() (string, error) {
	var r [4]byte
	if _, err := rand.Read(r[:]); err != nil {
		return "", err
	}
	// Time first, so ids sort in deletion order.
	return fmt.Sprintf("%016x%s", time.Now().UnixNano(), hex.EncodeToString(r[:])), nil
}

// Trash moves rel into its dataset's trash and returns the trash id.
// Nothing is copied or removed.
func Trash(root *safepath.Root, rel string) (string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", userr.Error("refusing to delete root")
	}
	info, err := root.FS().Lstat(rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	if isMountRoot(root, rel) {
		return "", userr.Error("this is a mounted dataset; it can't be deleted from here")
	}
	store, err := storeFor(root, rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	id, err := newTrashID()
	if err != nil {
		return "", err
	}
	dir := path.Join(internalDir(store, "trash"), id)
	if err := root.FS().MkdirAll(dir, 0o700); err != nil {
		return "", ErrNoTrash
	}

	// Record where it came from before moving it, so a crash in between
	// leaves a restorable record rather than an anonymous file.
	meta, _ := json.Marshal(TrashItem{ID: id, Path: rel, Deleted: time.Now().UTC(), Size: sizeOf(info), IsDir: info.IsDir()})
	if err := root.FS().WriteFile(path.Join(dir, "info.json"), meta, 0o600); err != nil {
		root.FS().RemoveAll(dir)
		return "", ErrNoTrash
	}
	if err := root.FS().Rename(rel, path.Join(dir, "item")); err != nil {
		root.FS().RemoveAll(dir)
		return "", safepath.Err(err)
	}
	return id, nil
}

func sizeOf(fi fs.FileInfo) int64 {
	if fi.IsDir() {
		return 0
	}
	return fi.Size()
}

// ListTrash returns every trashed item across all datasets, newest first.
func ListTrash(root *safepath.Root) ([]TrashItem, error) {
	items := []TrashItem{}
	for _, s := range stores(root) {
		ents, err := root.FS().Open(internalDir(s, "trash"))
		if err != nil {
			continue
		}
		names, _ := ents.Readdirnames(-1)
		ents.Close()
		for _, id := range names {
			if it, err := readTrash(root, s, id); err == nil {
				items = append(items, it)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Deleted.After(items[j].Deleted) })
	return items, nil
}

func readTrash(root *safepath.Root, store, id string) (TrashItem, error) {
	if !trashID.MatchString(id) {
		return TrashItem{}, os.ErrNotExist
	}
	raw, err := root.FS().ReadFile(path.Join(internalDir(store, "trash"), id, "info.json"))
	if err != nil {
		return TrashItem{}, err
	}
	var it TrashItem
	if err := json.Unmarshal(raw, &it); err != nil || it.ID != id {
		return TrashItem{}, os.ErrInvalid
	}
	it.store = store
	return it, nil
}

func findTrash(root *safepath.Root, id string) (TrashItem, error) {
	if !trashID.MatchString(id) {
		return TrashItem{}, userr.Error("no such item in trash")
	}
	for _, s := range stores(root) {
		if it, err := readTrash(root, s, id); err == nil {
			return it, nil
		}
	}
	return TrashItem{}, userr.Error("no such item in trash")
}

// Restore puts a trashed item back. If its old path is taken, the restored
// copy gets a "(restored)" suffix instead of overwriting anything.
func Restore(root *safepath.Root, id string) (string, error) {
	it, err := findTrash(root, id)
	if err != nil {
		return "", err
	}
	// The recorded path is data from disk; vet it like a request.
	target, err := safepath.User(it.Path)
	if err != nil || target == "." {
		return "", userr.Error("this item can't be restored to its old location")
	}
	if err := root.FS().MkdirAll(path.Dir(target), 0o755); err != nil {
		return "", safepath.Err(err)
	}
	target = freeName(root, target)
	dir := path.Join(internalDir(it.store, "trash"), id)
	if err := root.FS().Rename(path.Join(dir, "item"), target); err != nil {
		return "", safepath.Err(err)
	}
	root.FS().RemoveAll(dir)
	return target, nil
}

// freeName returns p, or p with " (restored)", " (restored 2)", ... before
// its extension, whichever doesn't exist yet.
func freeName(root *safepath.Root, p string) string {
	if _, err := root.FS().Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return p
	}
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for n := 1; ; n++ {
		suffix := " (restored)"
		if n > 1 {
			suffix = fmt.Sprintf(" (restored %d)", n)
		}
		cand := base + suffix + ext
		if _, err := root.FS().Lstat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
}

// Purge permanently removes one trashed item.
func Purge(root *safepath.Root, id string) error {
	it, err := findTrash(root, id)
	if err != nil {
		return err
	}
	return safepath.Err(root.FS().RemoveAll(path.Join(internalDir(it.store, "trash"), id)))
}

// EmptyTrash permanently removes everything in every trash.
func EmptyTrash(root *safepath.Root) error {
	items, _ := ListTrash(root)
	for _, it := range items {
		if err := root.FS().RemoveAll(path.Join(internalDir(it.store, "trash"), it.ID)); err != nil {
			return safepath.Err(err)
		}
	}
	return nil
}

// SweepTrash purges items deleted more than maxAge ago, plus leftovers with
// no readable record that are at least a day old.
func SweepTrash(root *safepath.Root, maxAge time.Duration) (int, error) {
	n := 0
	now := time.Now()
	for _, s := range stores(root) {
		base := internalDir(s, "trash")
		d, err := root.FS().Open(base)
		if err != nil {
			continue
		}
		names, _ := d.Readdirnames(-1)
		d.Close()
		for _, id := range names {
			dir := path.Join(base, id)
			it, err := readTrash(root, s, id)
			expired := err == nil && now.Sub(it.Deleted) > maxAge
			if err != nil {
				fi, serr := root.FS().Lstat(dir)
				expired = serr == nil && now.Sub(fi.ModTime()) > 24*time.Hour
			}
			if expired && root.FS().RemoveAll(dir) == nil {
				n++
			}
		}
	}
	return n, nil
}
