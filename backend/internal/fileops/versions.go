//go:build linux || darwin

package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Version retention: whichever limit is hit first.
const (
	KeepVersions   = 20
	VersionMaxAge  = 90 * 24 * time.Hour
	versionIDWidth = 16
)

// Version is an earlier copy of a file, kept when Strata replaced it.
type Version struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
	Size int64     `json:"size"`
}

var versionID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Layout: <store>/.strata/versions/<sha256(path)>/<unix-nanos hex>.
func versionDir(root *safepath.Root, rel string) (string, error) {
	store, err := storeFor(root, rel)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rel))
	return path.Join(internalDir(store, "versions"), hex.EncodeToString(sum[:])), nil
}

// snapshot keeps the current content of rel before it is replaced. A hard
// link costs nothing and leaves rel in place, so readers never see the file
// missing; if linking isn't possible, the bytes are copied.
func snapshot(root *safepath.Root, rel string) error {
	fi, err := root.FS().Lstat(rel)
	if err != nil || !fi.Mode().IsRegular() {
		return nil // nothing to keep
	}
	dir, err := versionDir(root, rel)
	if err != nil {
		return err
	}
	if err := root.FS().MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dst := path.Join(dir, fmt.Sprintf("%0*x", versionIDWidth, time.Now().UnixNano()))
	if err := root.FS().Link(rel, dst); err != nil {
		if err := copyFile(root, rel, dst, 0o600); err != nil {
			return err
		}
	}
	return pruneVersions(root, dir, time.Now())
}

func pruneVersions(root *safepath.Root, dir string, now time.Time) error {
	vs, err := readVersions(root, dir)
	if err != nil {
		return err
	}
	for i, v := range vs { // newest first
		if i >= KeepVersions || now.Sub(v.Time) > VersionMaxAge {
			root.FS().Remove(path.Join(dir, v.ID))
		}
	}
	return nil
}

func readVersions(root *safepath.Root, dir string) ([]Version, error) {
	d, err := root.FS().Open(dir)
	if err != nil {
		return nil, err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return nil, err
	}
	vs := make([]Version, 0, len(names))
	for _, n := range names {
		if !versionID.MatchString(n) {
			continue
		}
		ns, _ := strconv.ParseInt(n, 16, 64)
		fi, err := root.FS().Lstat(path.Join(dir, n))
		if err != nil {
			continue
		}
		vs = append(vs, Version{ID: n, Time: time.Unix(0, ns).UTC(), Size: fi.Size()})
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].ID > vs[j].ID })
	return vs, nil
}

// ListVersions returns rel's earlier copies, newest first.
func ListVersions(root *safepath.Root, rel string) ([]Version, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	dir, err := versionDir(root, rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	vs, err := readVersions(root, dir)
	if os.IsNotExist(err) {
		return []Version{}, nil
	}
	return vs, err
}

// OpenVersion opens one earlier copy of rel for reading.
func OpenVersion(root *safepath.Root, rel, id string) (*os.File, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	if !versionID.MatchString(id) {
		return nil, userr.Error("no such version")
	}
	dir, err := versionDir(root, rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	f, err := root.FS().Open(path.Join(dir, id))
	if os.IsNotExist(err) {
		return nil, userr.Error("no such version")
	}
	return f, err
}

// RestoreVersion makes an earlier copy current again. The content being
// replaced is itself kept as a version, so a restore can be undone.
func RestoreVersion(root *safepath.Root, rel, id string) error {
	f, err := OpenVersion(root, rel, id)
	if err != nil {
		return err
	}
	defer f.Close()
	return Save(root, rel, f)
}

// SweepVersions applies the age limit across every dataset.
func SweepVersions(root *safepath.Root) {
	now := time.Now()
	for _, s := range stores(root) {
		base := internalDir(s, "versions")
		d, err := root.FS().Open(base)
		if err != nil {
			continue
		}
		names, _ := d.Readdirnames(-1)
		d.Close()
		for _, n := range names {
			dir := path.Join(base, n)
			pruneVersions(root, dir, now)
			if vs, err := readVersions(root, dir); err == nil && len(vs) == 0 {
				root.FS().Remove(dir)
			}
		}
	}
}
