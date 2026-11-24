package preview

import (
	"encoding/binary"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
	"golang.org/x/text/encoding/charmap"
)

// wordDoc reads the opening text of a Word 97-2003 .doc: the FIB in the
// WordDocument stream points to a piece table in the 0Table or 1Table
// stream, and the pieces say where the main text lives.
func wordDoc(ra io.ReaderAt) (*Preview, error) {
	cf, err := mscfb.New(ra)
	if err != nil {
		return nil, ErrUnsupported
	}
	streams := map[string][]byte{}
	for f, err := cf.Next(); err == nil; f, err = cf.Next() {
		switch f.Name {
		case "WordDocument", "0Table", "1Table":
			if f.Size > maxPart {
				return nil, ErrUnsupported
			}
			b := make([]byte, f.Size)
			if _, err := io.ReadFull(f, b); err != nil {
				return nil, ErrUnsupported
			}
			streams[f.Name] = b
		}
	}
	text, ok := wordText(streams)
	if !ok {
		return nil, ErrUnsupported
	}
	blocks := wordBlocks(text)
	if len(blocks) == 0 {
		return nil, ErrUnsupported
	}
	return &Preview{Type: "doc", Blocks: blocks}, nil
}

// wordText returns up to a few thousand characters of the main document.
func wordText(s map[string][]byte) ([]rune, bool) {
	wd := s["WordDocument"]
	if len(wd) < 0x22 || binary.LittleEndian.Uint16(wd) != 0xA5EC {
		return nil, false
	}
	if binary.LittleEndian.Uint16(wd[2:]) < 101 { // Word 6/95 has no piece table here
		return nil, false
	}
	flags := binary.LittleEndian.Uint16(wd[0x0A:])
	if flags&0x0100 != 0 { // encrypted
		return nil, false
	}
	table := s["0Table"]
	if flags&0x0200 != 0 {
		table = s["1Table"]
	}
	// FibBase, then counted arrays of shorts and longs, then fc/lcb pairs.
	at := 0x20
	u16 := func(o int) (int, bool) {
		if o+2 > len(wd) {
			return 0, false
		}
		return int(binary.LittleEndian.Uint16(wd[o:])), true
	}
	u32 := func(b []byte, o int) (int, bool) {
		if o < 0 || o+4 > len(b) {
			return 0, false
		}
		return int(binary.LittleEndian.Uint32(b[o:])), true
	}
	csw, ok := u16(at)
	if !ok {
		return nil, false
	}
	at += 2 + csw*2
	cslw, ok := u16(at)
	if !ok {
		return nil, false
	}
	lw := at + 2
	at = lw + cslw*4
	ccpText, ok1 := u32(wd, lw+3*4)
	cbRgFcLcb, ok2 := u16(at)
	if !ok1 || !ok2 || cbRgFcLcb < 34 {
		return nil, false
	}
	fcClx, ok1 := u32(wd, at+2+33*8)
	lcbClx, ok2 := u32(wd, at+2+33*8+4)
	if !ok1 || !ok2 || lcbClx == 0 || fcClx+lcbClx > len(table) {
		return nil, false
	}
	clx := table[fcClx : fcClx+lcbClx]

	// Skip Prc entries (0x01) to the Pcdt (0x02).
	i := 0
	for i < len(clx) && clx[i] == 0x01 {
		if i+3 > len(clx) {
			return nil, false
		}
		i += 3 + int(binary.LittleEndian.Uint16(clx[i+1:]))
	}
	if i+5 > len(clx) || clx[i] != 0x02 {
		return nil, false
	}
	lcb, _ := u32(clx, i+1)
	plc := clx[i+5:]
	if lcb < 4 || lcb > len(plc) || (lcb-4)%12 != 0 {
		return nil, false
	}
	n := (lcb - 4) / 12
	limit := min(ccpText, maxChars*3)
	var out []rune
	for k := 0; k < n && len(out) < limit; k++ {
		cpStart, _ := u32(plc, k*4)
		cpEnd, _ := u32(plc, (k+1)*4)
		pcd := plc[(n+1)*4+k*8:]
		fc, _ := u32(pcd, 2)
		count := min(cpEnd-cpStart, limit-len(out))
		if count <= 0 {
			continue
		}
		if fc&0x40000000 != 0 { // 8-bit cp1252 text at fc/2
			off := (fc &^ 0x40000000) / 2
			if off+count > len(wd) {
				return nil, false
			}
			s, _ := charmap.Windows1252.NewDecoder().Bytes(wd[off : off+count])
			out = append(out, []rune(string(s))...)
		} else { // UTF-16LE
			if fc+count*2 > len(wd) {
				return nil, false
			}
			u := make([]uint16, count)
			for j := range u {
				u[j] = binary.LittleEndian.Uint16(wd[fc+j*2:])
			}
			out = append(out, utf16.Decode(u)...)
		}
	}
	return out, true
}

// wordBlocks splits Word's text into paragraphs, dropping field codes.
func wordBlocks(text []rune) []Block {
	var (
		out   []Block
		cur   strings.Builder
		chars int
		field []bool // per open field: still in its instructions?
	)
	flush := func() {
		t := strings.Join(strings.Fields(cur.String()), " ")
		if t != "" && chars < maxChars && len(out) < maxBlocks {
			t = clip(t, maxChars-chars)
			chars += len(t)
			out = append(out, Block{Text: t})
		}
		cur.Reset()
	}
	for _, r := range text {
		switch r {
		case 0x13: // field begins with its instructions
			field = append(field, true)
		case 0x14: // separator: the result follows
			if len(field) > 0 {
				field[len(field)-1] = false
			}
		case 0x15:
			if len(field) > 0 {
				field = field[:len(field)-1]
			}
		case '\r', 0x0C:
			flush()
		case 0x07, 0x0B, '\t', 0x1E:
			cur.WriteByte(' ')
		default:
			if len(field) > 0 && field[len(field)-1] {
				continue
			}
			if r >= 0x20 && r != 0x1F {
				cur.WriteRune(r)
			}
		}
	}
	flush()
	return out
}
