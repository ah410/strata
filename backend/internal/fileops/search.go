//go:build linux || darwin

package fileops

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Search limits keep one request from walking a whole pool.
const (
	SearchMaxResults = 500
	SearchMaxVisited = 200_000
)

var errStop = errors.New("stop")

// SearchResult lists matches and whether a limit cut the walk short.
type SearchResult struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// Search finds names under dir containing q, case-insensitively. The walk
// goes through the root's fs.FS, so symlinks are listed but never followed.
func Search(ctx context.Context, root *safepath.Root, dir, q string) (*SearchResult, error) {
	dir, err := safepath.User(dir)
	if err != nil {
		return nil, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if len([]rune(q)) < 2 {
		return nil, userr.Error("search needs at least 2 characters")
	}

	// Lstat, not Stat: a linked folder would walk wherever the link points.
	if fi, err := root.FS().Lstat(dir); err != nil {
		return nil, safepath.Err(err)
	} else if !fi.IsDir() {
		return nil, userr.Error("not a directory")
	}

	res := &SearchResult{Entries: []Entry{}}
	visited := 0
	err = fs.WalkDir(root.FS().FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil // unreadable subtree: skip, keep searching
		}
		if p == dir {
			return nil
		}
		if visited++; visited > SearchMaxVisited || ctx.Err() != nil {
			res.Truncated = true
			return errStop
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(name), q) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		res.Entries = append(res.Entries, Entry{
			Name: name, Path: p, IsDir: d.IsDir(), Size: sizeOf(fi),
			Modified: fi.ModTime(), Kind: classify(name, d.IsDir()),
		})
		if len(res.Entries) >= SearchMaxResults {
			res.Truncated = true
			return errStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, safepath.Err(err)
	}
	return res, nil
}
