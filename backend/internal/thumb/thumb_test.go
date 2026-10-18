package thumb

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"strata/internal/safepath"
)

func pngOf(w, h int, c color.Color) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestMakeFitsAndKeepsAlpha(t *testing.T) {
	out, err := Make(bytes.NewReader(pngOf(1000, 500, color.NRGBA{200, 10, 10, 255})))
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, _ := image.DecodeConfig(bytes.NewReader(out))
	if format != "jpeg" || cfg.Width != 256 || cfg.Height != 128 {
		t.Fatalf("got %s %dx%d", format, cfg.Width, cfg.Height)
	}
	out, _ = Make(bytes.NewReader(pngOf(40, 40, color.NRGBA{0, 0, 0, 0})))
	if cfg, format, _ := image.DecodeConfig(bytes.NewReader(out)); format != "png" || cfg.Width != 40 {
		t.Fatalf("transparent image: %s %dx%d", format, cfg.Width, cfg.Height)
	}
}

// A PNG header claiming 100k x 100k pixels must be refused before decoding.
func TestImageBombRefusedFromHeader(t *testing.T) {
	b := pngOf(1, 1, color.White)
	binary.BigEndian.PutUint32(b[16:], 100_000)
	binary.BigEndian.PutUint32(b[20:], 100_000)
	if _, err := Make(bytes.NewReader(b)); err != ErrUnsupported {
		t.Fatalf("bomb: %v", err)
	}
	if _, err := Make(bytes.NewReader([]byte("not an image"))); err != ErrUnsupported {
		t.Fatalf("garbage: %v", err)
	}
}

// jpegWithOrientation builds a wide JPEG whose EXIF says "rotate 90".
func jpegWithOrientation(o uint16) []byte {
	var img bytes.Buffer
	jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 400, 100)), nil)
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(tiff[18:], o)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}
	raw := img.Bytes()
	return append(append(append([]byte{}, raw[:2]...), append(app1, seg...)...), raw[2:]...)
}

func TestExifOrientationIsApplied(t *testing.T) {
	out, err := Make(bytes.NewReader(jpegWithOrientation(6)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != 64 || cfg.Height != 256 {
		t.Fatalf("rotated thumb is %dx%d, want 64x256", cfg.Width, cfg.Height)
	}
	// Truncated or hostile EXIF falls back to upright, never panics.
	for _, b := range [][]byte{{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}, jpegWithOrientation(6)[:30]} {
		if o := exifOrientation(b); o != 1 {
			t.Errorf("orientation %d from junk", o)
		}
	}
}

func TestCacheServesAndRefuses(t *testing.T) {
	dir := t.TempDir()
	root, _ := safepath.NewRoot(dir)
	os.WriteFile(filepath.Join(dir, "a.png"), pngOf(300, 300, color.White), 0o644)
	os.MkdirAll(filepath.Join(dir, ".strata"), 0o755)
	os.WriteFile(filepath.Join(dir, ".strata", "b.png"), pngOf(3, 3, color.White), 0o644)
	os.WriteFile(filepath.Join(dir, "doc.txt"), []byte("x"), 0o644)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "s.png"), pngOf(3, 3, color.White), 0o644)
	os.Symlink(filepath.Join(outside, "s.png"), filepath.Join(dir, "link.png"))

	c, err := New(filepath.Join(t.TempDir(), "thumbs"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a, ct, err := c.Get(context.Background(), root, "a.png")
	if err != nil || ct != "image/jpeg" {
		t.Fatalf("%v %s", err, ct)
	}
	b, _, _ := c.Get(context.Background(), root, "a.png")
	if !bytes.Equal(a, b) {
		t.Error("cache hit differs")
	}
	if ents, _ := os.ReadDir(c.dir); len(ents) != 1 {
		t.Errorf("cache holds %d files", len(ents))
	}
	for _, p := range []string{".strata/b.png", "doc.txt", "link.png", "../x.png", "missing.png"} {
		if _, _, err := c.Get(context.Background(), root, p); err == nil {
			t.Errorf("%s: thumbnail made", p)
		}
	}
}

func TestEvictionKeepsUnderLimit(t *testing.T) {
	c, _ := New(t.TempDir(), 1000)
	for i := range 10 {
		os.WriteFile(filepath.Join(c.dir, string(rune('a'+i))), make([]byte, 300), 0o600)
	}
	c.evict()
	var total int64
	ents, _ := os.ReadDir(c.dir)
	for _, e := range ents {
		fi, _ := e.Info()
		total += fi.Size()
	}
	if total > 900 {
		t.Fatalf("cache at %d bytes after eviction", total)
	}
}
