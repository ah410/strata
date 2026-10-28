package fileops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"io"
	"mime"
	"path"
	"strings"

	// Formats DecodeConfig can size; thumb registers the rest.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"strata/internal/safepath"
	"strata/internal/userr"
)

// Info is the detail panel for one file or folder.
type Info struct {
	Entry
	Type   string `json:"type,omitempty"`
	Items  int    `json:"items,omitempty"` // visible entries in a folder
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Describe returns what the info panel shows. Image dimensions come from the
// header only; the pixels are never decoded here.
func Describe(root *safepath.Root, rel string) (*Info, error) {
	e, err := Stat(root, rel)
	if err != nil {
		return nil, err
	}
	in := &Info{Entry: e}
	if e.IsDir {
		if l, err := List(root, e.Path); err == nil {
			in.Items = len(l.Entries)
		}
		return in, nil
	}
	in.Type = strings.Split(mime.TypeByExtension(strings.ToLower(path.Ext(e.Name))), ";")[0]
	if e.Kind == "image" {
		if f, err := root.FS().Open(e.Path); err == nil {
			if cfg, _, err := image.DecodeConfig(io.LimitReader(f, 1<<20)); err == nil {
				in.Width, in.Height = cfg.Width, cfg.Height
			}
			f.Close()
		}
	}
	return in, nil
}

// Checksum is the SHA-256 of a file, as hex.
func Checksum(ctx context.Context, root *safepath.Root, rel string, progress func(done, total int64)) (string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return "", err
	}
	f, err := root.FS().Open(rel)
	if err != nil {
		return "", safepath.Err(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", userr.Error("only files have a checksum")
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		done += int64(n)
		if progress != nil {
			progress(done, fi.Size())
		}
		if err == io.EOF {
			return hex.EncodeToString(h.Sum(nil)), nil
		}
		if err != nil {
			return "", err
		}
	}
}
