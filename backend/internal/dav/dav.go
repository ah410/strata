// Package dav serves the share over WebDAV, so it can be mounted in Finder,
// Explorer, a Linux file manager or a phone. Every operation goes through
// the same rules as the web UI: paths stay inside the root, Strata's own
// folder is invisible, deletes go to the trash, and overwrites keep the old
// version.
package dav

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"sync/atomic"

	"golang.org/x/net/webdav"

	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/web"
)

// Prefix is where the share is mounted.
const Prefix = web.DavPrefix

// Handler serves root under Prefix. Authentication is the caller's job.
func Handler(root *safepath.Root) http.Handler {
	h := &webdav.Handler{
		Prefix:     Prefix,
		FileSystem: &FS{root: root},
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("dav %s %s: %v", r.Method, r.URL.Path, err)
			}
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebDAV never uses POST; refusing it shuts out cross-site forms.
		if r.Method == http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Files are untrusted: a browser opening one gets it sandboxed and
		// as a download, never as a page running on Strata's origin.
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			ext := path.Ext(r.URL.Path)
			w.Header().Set("Content-Type", web.RawHeaders(w, ext, mime.TypeByExtension(ext)))
			w.Header().Set("Content-Disposition", "attachment")
		}
		if r.Method == http.MethodPut && r.Body != nil {
			body := &trackedBody{r: r.Body}
			r.Body = body
			r = r.WithContext(context.WithValue(r.Context(), bodyKey{}, body))
		}
		h.ServeHTTP(w, r)
	})
}

type bodyKey struct{}

// trackedBody remembers whether reading the request body failed, so a PUT
// cut off by a dropped connection doesn't replace a file with half of one.
type trackedBody struct {
	r      io.ReadCloser
	broken atomic.Bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && err != io.EOF {
		b.broken.Store(true)
	}
	return n, err
}

func (b *trackedBody) Close() error { return b.r.Close() }

// FS adapts the share to webdav.FileSystem.
type FS struct {
	root *safepath.Root
}

// rel maps a WebDAV name to a root-relative path. Strata's internal folder
// and temp files answer as if they didn't exist.
func (s *FS) rel(name string) (string, error) {
	rel, err := safepath.User(name)
	if err != nil {
		return "", os.ErrNotExist
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".strata-") {
			return "", os.ErrNotExist
		}
	}
	return rel, nil
}

func mapErr(err error) error {
	if errors.Is(safepath.Err(err), safepath.ErrEscape) {
		return os.ErrNotExist
	}
	return err
}

func (s *FS) Mkdir(_ context.Context, name string, _ os.FileMode) error {
	rel, err := s.rel(name)
	if err != nil {
		return err
	}
	return mapErr(s.root.FS().Mkdir(rel, 0o755))
}

func (s *FS) RemoveAll(_ context.Context, name string) error {
	rel, err := s.rel(name)
	if err != nil {
		return err
	}
	if rel == "." {
		return os.ErrPermission
	}
	if _, err := s.root.FS().Lstat(rel); err != nil {
		return mapErr(err)
	}
	// Deleting from a mounted drive is as easy to regret as in the browser.
	_, err = fileops.Trash(s.root, rel)
	if errors.Is(err, fileops.ErrNoTrash) {
		err = fileops.Delete(s.root, rel)
	}
	return mapErr(err)
}

func (s *FS) Rename(_ context.Context, oldName, newName string) error {
	from, err := s.rel(oldName)
	if err != nil {
		return err
	}
	to, err := s.rel(newName)
	if err != nil {
		return err
	}
	if from == "." || to == "." {
		return os.ErrPermission
	}
	if err := fileops.Move(s.root, from, to); err != nil {
		if _, serr := s.root.FS().Lstat(to); serr == nil {
			return os.ErrExist
		}
		return mapErr(err)
	}
	return nil
}

func (s *FS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	rel, err := s.rel(name)
	if err != nil {
		return nil, err
	}
	fi, err := s.root.FS().Stat(rel)
	return fi, mapErr(err)
}

func (s *FS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	rel, err := s.rel(name)
	if err != nil {
		return nil, err
	}
	if flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		f, err := s.root.FS().Open(rel)
		if err != nil {
			return nil, mapErr(err)
		}
		return &readFile{File: f, fs: s, rel: rel}, nil
	}
	_, statErr := s.root.FS().Lstat(rel)
	if flag&os.O_EXCL != 0 && statErr == nil {
		return nil, os.ErrExist
	}
	if flag&os.O_CREATE == 0 && statErr != nil {
		return nil, os.ErrNotExist
	}
	if flag&os.O_TRUNC == 0 && statErr == nil {
		// Writing into the middle of a file can't be made atomic.
		return nil, os.ErrPermission
	}
	failed := func() bool { return false }
	if b, ok := ctx.Value(bodyKey{}).(*trackedBody); ok {
		failed = b.broken.Load
	}
	a, err := fileops.CreateAtomic(s.root, rel, failed)
	if err != nil {
		return nil, mapErr(err)
	}
	return &writeFile{AtomicFile: a}, nil
}

// readFile lists directories through the root, hiding Strata's own files.
type readFile struct {
	*os.File
	fs  *FS
	rel string
}

func (f *readFile) Write([]byte) (int, error) { return 0, os.ErrPermission }

func (f *readFile) Readdir(int) ([]fs.FileInfo, error) {
	names, err := f.File.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, 0, len(names))
	for _, n := range names {
		if n == safepath.Internal || strings.HasPrefix(n, ".strata-") {
			continue
		}
		if fi, err := f.fs.root.FS().Lstat(path.Join(f.rel, n)); err == nil {
			out = append(out, fi)
		}
	}
	return out, nil
}

type writeFile struct {
	*fileops.AtomicFile
}

func (w *writeFile) Read([]byte) (int, error)           { return 0, os.ErrPermission }
func (w *writeFile) Seek(int64, int) (int64, error)     { return 0, os.ErrPermission }
func (w *writeFile) Readdir(int) ([]fs.FileInfo, error) { return nil, os.ErrPermission }
