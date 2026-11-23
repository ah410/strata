//go:build linux || darwin

package fileops

import (
	"container/heap"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// A walk of a large share takes minutes, so the storage dashboard reads
// from the last finished scan and a new one runs in the background when
// that gets old. Sizes are allocated blocks, not apparent sizes: with ZFS
// compression the two differ, and blocks are what fill the pool.

// SizeEntry is one file or folder in the storage dashboard.
type SizeEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	Files int64  `json:"files,omitempty"`
	Kind  string `json:"kind"`
}

// SizeReport is what the dashboard shows for one folder.
type SizeReport struct {
	Ready    bool      `json:"ready"`
	Scanning bool      `json:"scanning"`
	Seen     int64     `json:"seen"` // entries walked by the running scan
	Scanned  time.Time `json:"scanned"`

	Path     string      `json:"path"`
	Size     int64       `json:"size"`
	Files    int64       `json:"files"`
	Hidden   int64       `json:"hidden"` // dotfiles, which listings leave out
	Children []SizeEntry `json:"children"`
	Largest  []SizeEntry `json:"largest"`
	// Share-wide figures, the same at every level.
	Kinds    map[string]int64 `json:"kinds"`
	Trash    int64            `json:"trash"`
	Versions int64            `json:"versions"`
}

const (
	keepLargest  = 1000 // share-wide largest files kept per scan
	keepFiles    = 50   // largest direct files kept per folder
	showChildren = 100
	showLargest  = 25
)

type duNode struct {
	size, files, hidden int64
	dirs                []string // child folder names
	top                 []SizeEntry
}

type duScan struct {
	at              time.Time
	dirs            map[string]*duNode
	largest         []SizeEntry
	kinds           map[string]int64
	trash, versions int64
}

// Sizer scans the share on demand and caches the result.
type Sizer struct {
	root   *safepath.Root
	maxAge time.Duration

	mu      sync.Mutex
	last    *duScan
	running bool
	seen    atomic.Int64
}

// NewSizer returns a Sizer whose scans are refreshed once older than maxAge.
func NewSizer(root *safepath.Root, maxAge time.Duration) *Sizer {
	return &Sizer{root: root, maxAge: maxAge}
}

// Report describes rel from the last scan, starting a scan when there is
// none, it is stale, or rescan is set.
func (s *Sizer) Report(rel string, rescan bool) (*SizeReport, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	last := s.last
	if !s.running && (last == nil || rescan || time.Since(last.at) > s.maxAge) {
		s.running = true
		s.seen.Store(0)
		go s.scan()
	}
	r := &SizeReport{Scanning: s.running, Seen: s.seen.Load(), Path: apiPath(rel), Children: []SizeEntry{}, Largest: []SizeEntry{}}
	s.mu.Unlock()
	if last == nil {
		return r, nil
	}

	n, ok := last.dirs[rel]
	if !ok {
		return nil, userr.Error("this folder wasn't there at the last scan; rescan to include it")
	}
	r.Ready, r.Scanned = true, last.at
	r.Size, r.Files, r.Hidden = n.size, n.files, n.hidden
	r.Kinds, r.Trash, r.Versions = last.kinds, last.trash, last.versions
	for _, name := range n.dirs {
		p := path.Join(rel, name)
		c := last.dirs[p]
		r.Children = append(r.Children, SizeEntry{Name: name, Path: p, IsDir: true, Size: c.size, Files: c.files, Kind: "folder"})
	}
	r.Children = append(r.Children, n.top...)
	sort.Slice(r.Children, func(i, j int) bool { return r.Children[i].Size > r.Children[j].Size })
	if len(r.Children) > showChildren {
		r.Children = r.Children[:showChildren]
	}
	for _, f := range last.largest {
		if within(f.Path, rel) {
			r.Largest = append(r.Largest, f)
			if len(r.Largest) == showLargest {
				break
			}
		}
	}
	return r, nil
}

func (s *Sizer) scan() {
	w := &duWalk{
		root:  s.root,
		seen:  &s.seen,
		inode: map[[2]uint64]bool{},
		out:   &duScan{dirs: map[string]*duNode{}, kinds: map[string]int64{}},
	}
	w.dir(".", false)
	sort.Slice(w.big, func(i, j int) bool { return w.big[i].Size > w.big[j].Size })
	w.out.largest = w.big
	w.out.at = time.Now()

	s.mu.Lock()
	s.last, s.running = w.out, false
	s.mu.Unlock()
}

type duWalk struct {
	root  *safepath.Root
	seen  *atomic.Int64
	inode map[[2]uint64]bool
	out   *duScan
	big   fileHeap
}

// dir walks rel and returns its total allocated size and file count. Below
// a dotfolder everything counts as hidden.
func (w *duWalk) dir(rel string, inHidden bool) (int64, int64) {
	n := &duNode{}
	w.out.dirs[rel] = n
	d, err := w.root.FS().Open(rel)
	if err != nil {
		return 0, 0
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return 0, 0
	}
	var files fileHeap
	for _, name := range names {
		p := path.Join(rel, name)
		w.seen.Add(1)
		fi, err := w.root.FS().Lstat(p)
		if err != nil {
			continue
		}
		hidden := inHidden || strings.HasPrefix(name, ".")
		switch {
		case name == safepath.Internal:
			w.internal(p)
		case strings.HasPrefix(name, ".strata-"):
			// Temp and staging files: in flight, not the user's.
		case fi.IsDir():
			size, count := w.dir(p, hidden)
			n.size += size
			n.files += count
			if hidden {
				n.hidden += size
				delete(w.out.dirs, p)
			} else {
				n.dirs = append(n.dirs, name)
			}
		case fi.Mode().IsRegular():
			size := w.alloc(fi.Sys())
			n.size += size
			n.files++
			if hidden {
				n.hidden += size
				continue
			}
			kind := classify(name, false)
			w.out.kinds[kind] += size
			e := SizeEntry{Name: name, Path: p, Size: size, Kind: kind}
			push(&files, e, keepFiles)
			push(&w.big, e, keepLargest)
		}
	}
	n.top = files
	return n.size, n.files
}

// internal adds up a .strata folder's trash and version history.
func (w *duWalk) internal(rel string) {
	for sub, total := range map[string]*int64{"trash": &w.out.trash, "versions": &w.out.versions} {
		*total += w.tree(path.Join(rel, sub))
	}
}

// tree is the allocated size of everything below rel.
func (w *duWalk) tree(rel string) int64 {
	fi, err := w.root.FS().Lstat(rel)
	if err != nil {
		return 0
	}
	if !fi.IsDir() {
		if fi.Mode().IsRegular() {
			return w.alloc(fi.Sys())
		}
		return 0
	}
	d, err := w.root.FS().Open(rel)
	if err != nil {
		return 0
	}
	names, _ := d.Readdirnames(-1)
	d.Close()
	var n int64
	for _, name := range names {
		n += w.tree(path.Join(rel, name))
	}
	return n
}

// alloc is the file's allocated size, counting each hard-linked inode once.
func (w *duWalk) alloc(sys any) int64 {
	st, ok := sys.(*syscall.Stat_t)
	if !ok {
		return 0
	}
	if st.Nlink > 1 {
		key := [2]uint64{uint64(st.Dev), uint64(st.Ino)}
		if w.inode[key] {
			return 0
		}
		w.inode[key] = true
	}
	return int64(st.Blocks) * 512
}

// fileHeap keeps the n largest entries, smallest on top.
type fileHeap []SizeEntry

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(x any)        { *h = append(*h, x.(SizeEntry)) }
func (h *fileHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func push(h *fileHeap, e SizeEntry, keep int) {
	if h.Len() < keep {
		heap.Push(h, e)
	} else if (*h)[0].Size < e.Size {
		(*h)[0] = e
		heap.Fix(h, 0)
	}
}
