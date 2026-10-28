package fileops

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"
)

func TestDescribe(t *testing.T) {
	root, dir := share(t)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 640, 480)))
	write(t, dir, "pics/a.png", buf.String())
	write(t, dir, "pics/b.txt", "x")
	write(t, dir, "pics/.hidden", "x")

	in, err := Describe(root, "pics/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if in.Width != 640 || in.Height != 480 || in.Type != "image/png" || in.Kind != "image" {
		t.Fatalf("image info = %+v", in)
	}
	d, err := Describe(root, "pics")
	if err != nil || !d.IsDir || d.Items != 2 {
		t.Fatalf("folder info = %+v %v", d, err)
	}
	if _, err := Describe(root, ".strata"); err == nil {
		t.Fatal("internal dir described")
	}
}

func TestChecksum(t *testing.T) {
	root, dir := share(t)
	write(t, dir, "f", "hello\n")
	var last int64
	sum, err := Checksum(context.Background(), root, "f", func(d, _ int64) { last = d })
	if err != nil {
		t.Fatal(err)
	}
	if sum != "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03" || last != 6 {
		t.Fatalf("sum %s, progress %d", sum, last)
	}
	write(t, dir, "d/x", "x")
	if _, err := Checksum(context.Background(), root, "d", nil); err == nil {
		t.Fatal("folder checksummed")
	}
}
