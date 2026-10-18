// Package thumb makes small previews of images for the grid view.
//
// Images are untrusted input: a 1 KB PNG can claim to be 100k x 100k pixels
// and ask the decoder for 40 GB. The header is checked before any decoding,
// inputs are capped, and one decode runs at a time.
package thumb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "image/gif"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"strata/internal/safepath"
	"strata/internal/userr"
)

const (
	Size      = 256
	MaxBytes  = 50 << 20
	MaxPixels = 50_000_000
)

// ErrUnsupported means there is no thumbnail for this file; show an icon.
var ErrUnsupported = userr.Error("no thumbnail for this file")

var formats = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}

// Cache makes and keeps thumbnails in dir, holding it under limit bytes.
type Cache struct {
	dir    string
	limit  int64
	slots  chan struct{}
	writes atomic.Int64
	mu     sync.Mutex // one eviction pass at a time
}

// New returns a cache in dir, creating it if needed.
func New(dir string, limit int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Cache{dir: dir, limit: limit, slots: make(chan struct{}, 1)}, nil
}

// Get returns a thumbnail for rel and its content type.
func (c *Cache) Get(ctx context.Context, root *safepath.Root, rel string) ([]byte, string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, "", err
	}
	if !formats[strings.ToLower(filepath.Ext(rel))] {
		return nil, "", ErrUnsupported
	}
	fi, err := root.FS().Lstat(rel)
	if err != nil {
		return nil, "", safepath.Err(err)
	}
	if !fi.Mode().IsRegular() || fi.Size() > MaxBytes {
		return nil, "", ErrUnsupported
	}

	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%d", rel, fi.ModTime().UnixNano(), fi.Size(), Size)))
	file := filepath.Join(c.dir, hex.EncodeToString(sum[:16]))
	if b, err := os.ReadFile(file); err == nil {
		now := time.Now()
		os.Chtimes(file, now, now) // recently used survives eviction
		return b, sniff(b), nil
	}

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}

	f, err := root.FS().Open(rel)
	if err != nil {
		return nil, "", safepath.Err(err)
	}
	defer f.Close()
	b, err := Make(io.LimitReader(f, MaxBytes))
	if err != nil {
		return nil, "", err
	}
	c.store(file, b)
	return b, sniff(b), nil
}

func sniff(b []byte) string {
	if bytes.HasPrefix(b, []byte("\x89PNG")) {
		return "image/png"
	}
	return "image/jpeg"
}

// Make decodes an image and returns a JPEG (or PNG, if it has transparency)
// that fits in Size x Size.
func Make(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, ErrUnsupported
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUnsupported
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > Size || h > Size {
		if w >= h {
			w, h = Size, max(1, h*Size/w)
		} else {
			w, h = max(1, w*Size/h), Size
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	// Rotate after scaling, on the small image.
	small := orient(dst, exifOrientation(raw))

	var out bytes.Buffer
	if dst.Opaque() {
		err = jpeg.Encode(&out, small, &jpeg.Options{Quality: 80})
	} else {
		err = png.Encode(&out, small)
	}
	return out.Bytes(), err
}

// store writes atomically, then trims the cache now and then.
func (c *Cache) store(file string, b []byte) {
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp.Name(), file) != nil {
		os.Remove(tmp.Name())
		return
	}
	if c.writes.Add(1)%32 == 1 {
		go c.evict()
	}
}

// evict removes least recently used thumbnails until under the limit.
func (c *Cache) evict() {
	if !c.mu.TryLock() {
		return
	}
	defer c.mu.Unlock()
	var files []fs.FileInfo
	var total int64
	ents, _ := os.ReadDir(c.dir)
	for _, e := range ents {
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() {
			files = append(files, fi)
			total += fi.Size()
		}
	}
	if total <= c.limit {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime().Before(files[j].ModTime()) })
	for _, fi := range files {
		if total <= c.limit*9/10 {
			break
		}
		if os.Remove(filepath.Join(c.dir, fi.Name())) == nil {
			total -= fi.Size()
		}
	}
}

// orient applies an EXIF orientation (1-8) so phone photos stand upright.
func orient(src image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if o >= 5 {
		w, h = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = b.Dx()-1-x, y
			case 3:
				dx, dy = b.Dx()-1-x, b.Dy()-1-y
			case 4:
				dx, dy = x, b.Dy()-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = b.Dy()-1-y, x
			case 7:
				dx, dy = b.Dy()-1-y, b.Dx()-1-x
			case 8:
				dx, dy = y, b.Dx()-1-x
			}
			dst.Set(dx, dy, color.RGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)))
		}
	}
	return dst
}

// exifOrientation reads the Orientation tag from a JPEG's APP1 segment, or
// returns 1. It only walks bounded offsets inside the buffer it was given.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		n := int(b[i+2])<<8 | int(b[i+3])
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		if marker == 0xDA { // image data starts; no EXIF before it
			return 1
		}
		i += 2 + n
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(p []byte) int { return int(p[0]) | int(p[1])<<8 }
		u32 = func(p []byte) int { return u16(p) | u16(p[2:])<<16 }
	case "MM":
		u16 = func(p []byte) int { return int(p[0])<<8 | int(p[1]) }
		u32 = func(p []byte) int { return u16(p)<<16 | u16(p[2:]) }
	default:
		return 1
	}
	off := u32(t[4:])
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := u16(t[off:])
	for k := 0; k < count; k++ {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if u16(t[e:]) == 0x0112 {
			return u16(t[e+8:])
		}
	}
	return 1
}
