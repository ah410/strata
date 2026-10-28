//go:build linux || darwin

package fileops

import (
	"archive/zip"
	"context"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// ZipMaxItems caps how many selected paths one archive may name.
const ZipMaxItems = 1000

// ZipCheck vets the selection before any bytes are sent, since an error
// after the response starts can only truncate the archive.
func ZipCheck(root *safepath.Root, paths []string) ([]string, error) {
	if len(paths) == 0 || len(paths) > ZipMaxItems {
		return nil, userr.Error("select between 1 and 1000 items")
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := safepath.User(p)
		if err != nil {
			return nil, err
		}
		fi, err := root.FS().Lstat(rel)
		if err != nil {
			return nil, safepath.Err(err)
		}
		// The walk would follow a top-level link; refuse it instead.
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			return nil, userr.Error("only files and folders can be downloaded")
		}
		out = append(out, rel)
	}
	return out, nil
}

// Zip streams the vetted paths as one archive. Folders are walked without
// following symlinks; Strata's own files are left out.
func Zip(ctx context.Context, root *safepath.Root, paths []string, w io.Writer) error {
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	for _, rel := range paths {
		// Entries are named from the item's own name down, so a selection
		// from /a/b unpacks as b/..., never as a full share path.
		top := uniqueName(used, path.Base(rel))
		if rel == "." {
			top = "files"
		}
		err := fs.WalkDir(root.FS().FS(), rel, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable: skip
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if p != rel && internal(d.Name()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			name := top
			if p != rel {
				name = top + "/" + strings.TrimPrefix(p, strings.TrimPrefix(rel+"/", "./"))
			}
			switch {
			case d.IsDir():
				_, err := zw.CreateHeader(&zip.FileHeader{Name: name + "/", Method: zip.Store})
				return err
			case !d.Type().IsRegular():
				return nil // symlinks and devices stay out
			}
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			hdr, err := zip.FileInfoHeader(fi)
			if err != nil {
				return err
			}
			hdr.Name = name
			hdr.Method = zip.Deflate
			if compressed[strings.ToLower(path.Ext(name))] {
				hdr.Method = zip.Store
			}
			dst, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := root.FS().Open(p)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, err = io.Copy(dst, f)
			return err
		})
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

// internal matches Strata's trash/version directory and its temp files.
func internal(name string) bool {
	return name == safepath.Internal || strings.HasPrefix(name, ".strata-")
}

func uniqueName(used map[string]bool, name string) string {
	cand := name
	for n := 2; used[cand]; n++ {
		ext := path.Ext(name)
		cand = strings.TrimSuffix(name, ext) + " (" + strconv.Itoa(n) + ")" + ext
	}
	used[cand] = true
	return cand
}

// Already-compressed formats gain nothing from deflate.
var compressed = map[string]bool{
	".zip": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true, ".zst": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".avif": true, ".heic": true,
	".mp4": true, ".mkv": true, ".mov": true, ".webm": true, ".avi": true,
	".mp3": true, ".m4a": true, ".ogg": true, ".opus": true, ".flac": true,
	".xlsx": true, ".docx": true, ".pptx": true, ".odt": true, ".pdf": true,
}

// Compress writes the vetted paths as a zip into dir, named after the single
// item or "Archive", and returns the new file's path. The zip is built in a
// hidden temp file and only renamed into place when complete.
func Compress(ctx context.Context, root *safepath.Root, paths []string, dir string, progress func(done, total int64)) (string, error) {
	dir, err := safepath.User(dir)
	if err != nil {
		return "", err
	}
	var total int64
	for _, p := range paths {
		fs.WalkDir(root.FS().FS(), p, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					total += fi.Size()
				}
			}
			return nil
		})
	}
	if u, err := Usage(root); err == nil && uint64(total) > u.Free {
		return "", userr.Error("not enough free space for the archive")
	}

	tmp, tmpName, err := createTemp(root, dir)
	if err != nil {
		return "", safepath.Err(err)
	}
	defer root.FS().Remove(tmpName) // no-op once renamed
	w := &progressWriter{w: tmp, total: total, report: progress}
	if err := Zip(ctx, root, paths, w); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	name := "Archive"
	if len(paths) == 1 && paths[0] != "." {
		name = path.Base(paths[0])
	}
	for n := 1; n < 10000; n++ {
		cand := path.Join(dir, name+".zip")
		if n > 1 {
			cand = path.Join(dir, name+" ("+strconv.Itoa(n)+").zip")
		}
		if _, err := root.FS().Lstat(cand); err == nil {
			continue
		}
		if err := root.FS().Rename(tmpName, cand); err != nil {
			return "", safepath.Err(err)
		}
		return apiPath(cand), nil
	}
	return "", userr.Error("too many archives with this name")
}

type progressWriter struct {
	w      io.Writer
	n      int64
	total  int64
	report func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.report != nil {
		p.report(min(p.n, p.total), p.total)
	}
	return n, err
}
