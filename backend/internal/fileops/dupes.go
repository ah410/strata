//go:build linux || darwin

package fileops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"syscall"

	"strata/internal/safepath"
)

// DupeGroup is a set of files with identical contents.
type DupeGroup struct {
	Size   int64    `json:"size"`
	Paths  []string `json:"paths"`
	Wasted int64    `json:"wasted"` // every copy but one
}

// MaxDupeGroups caps the result; the biggest wins come first.
const MaxDupeGroups = 300

// Duplicates finds files below rel with the same contents. Files are first
// grouped by size, then by a hash of their first 64 KB, and only the files
// still tied are hashed in full, so most of the share is never read.
// Hard links to one inode take no extra space and aren't reported.
func Duplicates(ctx context.Context, root *safepath.Root, rel string, minSize int64, progress func(done, total int64)) ([]DupeGroup, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	if minSize < 1 {
		minSize = 1
	}
	bySize := map[int64][]string{}
	inodes := map[[2]uint64]bool{}
	var seen int64
	err = fs.WalkDir(root.FS().FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p != rel && (strings.HasPrefix(d.Name(), ".") || internal(d.Name())) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil || fi.Size() < minSize {
			return nil
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
			key := [2]uint64{uint64(st.Dev), uint64(st.Ino)}
			if inodes[key] {
				return nil
			}
			inodes[key] = true
		}
		bySize[fi.Size()] = append(bySize[fi.Size()], p)
		seen++
		if progress != nil && seen%500 == 0 {
			progress(0, 0)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Only same-size files can match; that's what gets read.
	var total, done int64
	for size, ps := range bySize {
		if len(ps) > 1 {
			total += size * int64(len(ps))
		}
	}
	report := func(n int64) {
		done += n
		if progress != nil {
			progress(min(done, total), total)
		}
	}

	var groups []DupeGroup
	for size, ps := range bySize {
		if len(ps) < 2 {
			continue
		}
		heads := map[string][]string{}
		for _, p := range ps {
			h, err := hashFile(ctx, root, p, 64<<10)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			heads[h] = append(heads[h], p)
		}
		report(min(size, 64<<10) * int64(len(ps)))
		for _, tied := range heads {
			if len(tied) < 2 {
				continue
			}
			full := map[string][]string{}
			if size <= 64<<10 {
				full[""] = tied // the head was the whole file
			} else {
				for _, p := range tied {
					h, err := hashFile(ctx, root, p, -1)
					if err != nil {
						if ctx.Err() != nil {
							return nil, ctx.Err()
						}
						continue
					}
					full[h] = append(full[h], p)
					report(size - 64<<10)
				}
			}
			for _, same := range full {
				if len(same) > 1 {
					sort.Strings(same)
					for i := range same {
						same[i] = apiPath(same[i])
					}
					groups = append(groups, DupeGroup{Size: size, Paths: same, Wasted: size * int64(len(same)-1)})
				}
			}
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Wasted != groups[j].Wasted {
			return groups[i].Wasted > groups[j].Wasted
		}
		return groups[i].Paths[0] < groups[j].Paths[0]
	})
	if len(groups) > MaxDupeGroups {
		groups = groups[:MaxDupeGroups]
	}
	if groups == nil {
		groups = []DupeGroup{}
	}
	return groups, nil
}

// hashFile hashes the first limit bytes of p, or all of it when limit < 0.
func hashFile(ctx context.Context, root *safepath.Root, p string, limit int64) (string, error) {
	f, err := root.FS().Open(path.Clean(p))
	if err != nil {
		return "", err
	}
	defer f.Close()
	var r io.Reader = &ctxReader{ctx: ctx, r: f}
	if limit >= 0 {
		r = io.LimitReader(r, limit)
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
