//go:build linux || darwin

package fileops

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Each dataset keeps its own .strata directory at its top, so moving a file
// into the trash or versions is a rename on the same filesystem: instant,
// and never a copy. A "store" is that top directory, relative to the root.

// ErrNoTrash means the item's dataset has nowhere writable for a trash.
var ErrNoTrash = userr.Error("this location has no trash; delete permanently instead")

func dev(root *safepath.Root, rel string) (uint64, error) {
	fi, err := root.FS().Lstat(rel)
	if err != nil {
		return 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, os.ErrInvalid
	}
	return uint64(st.Dev), nil
}

// storeFor returns the top of the dataset holding rel's parent directory:
// the highest ancestor still on the same filesystem.
func storeFor(root *safepath.Root, rel string) (string, error) {
	dir := path.Dir(rel)
	d, err := dev(root, dir)
	if err != nil {
		return "", err
	}
	cur := dir
	for cur != "." {
		parent := path.Dir(cur)
		pd, err := dev(root, parent)
		if err != nil || pd != d {
			return cur, nil
		}
		cur = parent
	}
	return ".", nil
}

// isMountRoot reports whether rel is itself the top of a mounted dataset.
func isMountRoot(root *safepath.Root, rel string) bool {
	a, err1 := dev(root, rel)
	b, err2 := dev(root, path.Dir(rel))
	return err1 == nil && err2 == nil && a != b
}

// stores lists every place a .strata directory may live: the root plus each
// filesystem mounted beneath it.
func stores(root *safepath.Root) []string {
	set := map[string]bool{".": true}
	f, err := os.Open("/proc/self/mountinfo")
	if err == nil {
		defer f.Close()
		prefix := root.Path() + string(filepath.Separator)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 5 {
				continue
			}
			mp := unescapeMount(fields[4])
			if strings.HasPrefix(mp, prefix) {
				set[filepath.ToSlash(strings.TrimPrefix(mp, prefix))] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// unescapeMount decodes mountinfo's octal escapes (\040 for space, etc).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func internalDir(store, sub string) string {
	return path.Join(store, safepath.Internal, sub)
}
