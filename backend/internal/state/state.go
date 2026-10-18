// Package state keeps the small amount of per-install data Strata remembers
// between restarts: favorites and recently opened files. It is one JSON
// file, rewritten atomically; at this size a database would only add risk.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	MaxRecent    = 50
	MaxFavorites = 200
)

// Recent is a file opened or saved in Strata.
type Recent struct {
	Path string    `json:"path"`
	Time time.Time `json:"time"`
}

type data struct {
	Favorites []string `json:"favorites"`
	Recent    []Recent `json:"recent"`
}

// Store is safe for concurrent use. A nil *Store is a valid, empty store
// that remembers nothing (no config volume).
type Store struct {
	file string
	mu   sync.Mutex
	d    data
}

// Open loads dir/state.json, starting empty if it doesn't exist.
func Open(dir string) (*Store, error) {
	s := &Store{file: filepath.Join(dir, "state.json")}
	raw, err := os.ReadFile(s.file)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &s.d); err != nil {
			// A damaged file costs favorites, not startup; keep it aside.
			os.Rename(s.file, s.file+".bad")
			s.d = data{}
		}
	}
	return s, nil
}

// Enabled reports whether anything is remembered.
func (s *Store) Enabled() bool { return s != nil }

// Snapshot returns copies of the favorites and recent lists.
func (s *Store) Snapshot() ([]string, []Recent) {
	if s == nil {
		return []string{}, []Recent{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.d.Favorites...), append([]Recent{}, s.d.Recent...)
}

// SetFavorite adds or removes p.
func (s *Store) SetFavorite(p string, on bool) error {
	if s == nil {
		return nil
	}
	if slices.Contains(s.favorites(), p) == on {
		return nil
	}
	return s.update(func(d *data) {
		d.Favorites = slices.DeleteFunc(d.Favorites, func(f string) bool { return f == p })
		if on {
			d.Favorites = append(d.Favorites, p)
			if len(d.Favorites) > MaxFavorites {
				d.Favorites = d.Favorites[len(d.Favorites)-MaxFavorites:]
			}
		}
	})
}

func (s *Store) favorites() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Favorites
}

// Touch moves p to the front of the recent list.
func (s *Store) Touch(p string) error {
	if s == nil {
		return nil
	}
	return s.update(func(d *data) {
		d.Recent = slices.DeleteFunc(d.Recent, func(r Recent) bool { return r.Path == p })
		d.Recent = append([]Recent{{Path: p, Time: time.Now().UTC()}}, d.Recent...)
		if len(d.Recent) > MaxRecent {
			d.Recent = d.Recent[:MaxRecent]
		}
	})
}

// Rename follows a move, so favorites and recents keep pointing at it.
func (s *Store) Rename(from, to string) error {
	if s == nil {
		return nil
	}
	moved := func(p string) (string, bool) {
		if p == from {
			return to, true
		}
		if len(p) > len(from) && p[:len(from)+1] == from+"/" {
			return to + p[len(from):], true
		}
		return p, false
	}
	return s.update(func(d *data) {
		for i, f := range d.Favorites {
			d.Favorites[i], _ = moved(f)
		}
		for i := range d.Recent {
			d.Recent[i].Path, _ = moved(d.Recent[i].Path)
		}
	})
}

func (s *Store) update(fn func(*data)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := data{Favorites: slices.Clone(s.d.Favorites), Recent: slices.Clone(s.d.Recent)}
	fn(&next)
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.file), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
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
	if err := os.Rename(tmp.Name(), s.file); err != nil {
		return err
	}
	s.d = next
	return nil
}
