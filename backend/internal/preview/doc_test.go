package preview

import (
	"bytes"
	"strings"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestRTF(t *testing.T) {
	root, dir := setup(t)
	put(t, dir, "a.rtf", []byte(strings.ReplaceAll(`{\rtf1\ansi\ansicpg1252{\fonttbl\f0\fswiss Helvetica;}{\colortbl;\red255\green0\blue0;}
{\*\generator Riched20;}{\info{\title Secret title}}
\pard\b Quarterly report\b0\par
Revenue grew 12\'25 in caf\'e9s \{braces\} and back\\slash.\line
Second line with @u8364?uro and\tab Tab.\
Cocoa line break.\par
{\header Page header text}Body after header.\par}`, "@u", "\\u")))
	p, err := Make(root, "a.rtf")
	if err != nil {
		t.Fatal(err)
	}
	want := "Quarterly report|Revenue grew 12% in cafés {braces} and back\\slash.|Second line with €uro and Tab.|Cocoa line break.|Body after header."
	if p.Type != "doc" || texts(p.Blocks) != want {
		t.Fatalf("got %q\nwant %q", texts(p.Blocks), want)
	}
}

func TestRTFUnicodeBounds(t *testing.T) {
	// Out-of-range \uN is ignored, negative values wrap to 16 bits, \ucN is clamped.
	in := strings.ReplaceAll(`{\rtf1 a@u-4?b@u9999999999?c@uc999999999 d@uc1 @u65?e\par}`, "@u", "\\u")
	p, err := rtf(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if want := "a" + string(rune(0xFFFC)) + "b?cdAe"; p.Blocks[0].Text != want {
		t.Fatalf("got %q, want %q", p.Blocks[0].Text, want)
	}
}

func TestRTFRejectsNonRTF(t *testing.T) {
	root, dir := setup(t)
	put(t, dir, "fake.rtf", []byte("just text"))
	if _, err := Make(root, "fake.rtf"); err != ErrUnsupported {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestWordDoc(t *testing.T) {
	root, dir := setup(t)
	// Field: instructions are hidden, the result is kept.
	ansi := "Quarterly report\rRevenue grew \x13 HYPERLINK \"x\" \x14twelve\x15 percent.\r"
	wide := "Ünïcode tail\r"
	put(t, dir, "a.doc", wordFile(t, ansi, wide, 193, 0))
	p, err := Make(root, "a.doc")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Quarterly report|Revenue grew twelve percent.|Ünïcode tail"; p.Type != "doc" || texts(p.Blocks) != want {
		t.Fatalf("got %q, want %q", texts(p.Blocks), want)
	}
}

func TestWordDocFallsBackToIcon(t *testing.T) {
	root, dir := setup(t)
	cases := map[string][]byte{
		"word95.doc":    wordFile(t, "old\r", "", 100, 0),
		"encrypted.doc": wordFile(t, "secret\r", "", 193, 0x0100),
		"garbage.doc":   bytes.Repeat([]byte{0xD0, 0xCF}, 600),
		"truncated.doc": wordFile(t, "text\r", "", 193, 0)[:1500],
	}
	for name, b := range cases {
		put(t, dir, name, b)
		if _, err := Make(root, name); err != ErrUnsupported {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

// wordFile builds a minimal Word 97 .doc: a compound file holding a
// WordDocument stream (FIB plus text) and a 1Table stream (piece table).
// ansi is stored as an 8-bit piece, wide as a UTF-16 piece.
func wordFile(t *testing.T, ansi, wide string, nFib, flags uint16) []byte {
	t.Helper()
	le := binary.LittleEndian
	wd := make([]byte, 4096)
	le.PutUint16(wd[0:], 0xA5EC)
	le.PutUint16(wd[2:], nFib)
	le.PutUint16(wd[0x0A:], flags|0x0200) // text pieces live in 1Table
	le.PutUint16(wd[0x20:], 14)          // csw
	le.PutUint16(wd[0x3E:], 22)          // cslw
	w16 := utf16.Encode([]rune(wide))
	le.PutUint32(wd[0x4C:], uint32(len(ansi)+len(w16))) // ccpText
	le.PutUint16(wd[0x98:], 93)                          // cbRgFcLcb
	const ansiAt, wideAt = 0x800, 0xA00
	copy(wd[ansiAt:], ansi)
	for i, u := range w16 {
		le.PutUint16(wd[wideAt+i*2:], u)
	}

	var plc []byte
	cps := []uint32{0, uint32(len(ansi)), uint32(len(ansi) + len(w16))}
	for _, cp := range cps {
		plc = le.AppendUint32(plc, cp)
	}
	pcd := func(fc uint32) []byte { return le.AppendUint16(le.AppendUint32(le.AppendUint16(nil, 0), fc), 0) }
	plc = append(plc, pcd(ansiAt*2|0x40000000)...)
	plc = append(plc, pcd(wideAt)...)
	clx := append([]byte{0x02}, le.AppendUint32(nil, uint32(len(plc)))...)
	clx = append(clx, plc...)
	table := make([]byte, 4096)
	copy(table, clx)
	le.PutUint32(wd[0x1A2:], 0)                // fcClx
	le.PutUint32(wd[0x1A6:], uint32(len(clx))) // lcbClx
	return compoundFile(map[string][]byte{"WordDocument": wd, "1Table": table})
}

// compoundFile writes a version 3 compound file with full-size streams
// (each at least 4096 bytes, so no mini stream is needed).
func compoundFile(streams map[string][]byte) []byte {
	le := binary.LittleEndian
	const sec = 512
	const end, free, fatSect, none = 0xFFFFFFFE, 0xFFFFFFFF, 0xFFFFFFFD, 0xFFFFFFFF
	names := []string{"1Table", "WordDocument"}
	fat := make([]uint32, sec/4)
	for i := range fat {
		fat[i] = free
	}
	fat[0], fat[1] = fatSect, end // FAT, directory
	next := uint32(2)
	starts := map[string]uint32{}
	var body []byte
	for _, n := range names {
		b := streams[n]
		count := uint32((len(b) + sec - 1) / sec)
		starts[n] = next
		for i := uint32(0); i < count; i++ {
			fat[next+i] = next + i + 1
		}
		fat[next+count-1] = end
		next += count
		padded := make([]byte, count*sec)
		copy(padded, b)
		body = append(body, padded...)
	}

	h := make([]byte, sec)
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	le.PutUint16(h[0x18:], 0x003E)
	le.PutUint16(h[0x1A:], 3)
	le.PutUint16(h[0x1C:], 0xFFFE)
	le.PutUint16(h[0x1E:], 9)
	le.PutUint16(h[0x20:], 6)
	le.PutUint32(h[0x2C:], 1) // FAT sectors
	le.PutUint32(h[0x30:], 1) // first directory sector
	le.PutUint32(h[0x38:], 4096)
	le.PutUint32(h[0x3C:], end)
	le.PutUint32(h[0x44:], end)
	for i := 0; i < 109; i++ {
		le.PutUint32(h[0x4C+i*4:], free)
	}
	le.PutUint32(h[0x4C:], 0)

	entry := func(name string, typ byte, left, right, child, start uint32, size uint64) []byte {
		e := make([]byte, 128)
		u := utf16.Encode([]rune(name))
		for i, c := range u {
			le.PutUint16(e[i*2:], c)
		}
		le.PutUint16(e[64:], uint16((len(u)+1)*2))
		e[66], e[67] = typ, 1
		le.PutUint32(e[68:], left)
		le.PutUint32(e[72:], right)
		le.PutUint32(e[76:], child)
		le.PutUint32(e[116:], start)
		le.PutUint64(e[120:], size)
		return e
	}
	dir := entry("Root Entry", 5, none, none, 2, end, 0)
	dir = append(dir, entry("1Table", 2, none, none, none, starts["1Table"], uint64(len(streams["1Table"])))...)
	dir = append(dir, entry("WordDocument", 2, 1, none, none, starts["WordDocument"], uint64(len(streams["WordDocument"])))...)
	dir = append(dir, make([]byte, 128)...)
	for i := 3; i < 4; i++ {
		le.PutUint32(dir[i*128+68:], none)
		le.PutUint32(dir[i*128+72:], none)
		le.PutUint32(dir[i*128+76:], none)
	}

	fatBytes := make([]byte, sec)
	for i, v := range fat {
		le.PutUint32(fatBytes[i*4:], v)
	}
	out := append(h, fatBytes...)
	out = append(out, dir...)
	return append(out, body...)
}
