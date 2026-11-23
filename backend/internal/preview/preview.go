// Package preview pulls a small, text-only peek out of documents for the
// grid view: the opening of a text file, the corner of a spreadsheet, the
// first paragraphs of a Word or OpenDocument file, the first slide of a
// deck. Nothing is rendered here; the browser lays it out as a mini page.
//
// Files are untrusted. Each format is read through hard caps on how much is
// decompressed and parsed, so a crafted document costs at most a few MB.
package preview

import (
	"archive/zip"
	"bytes"
	"container/list"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"unicode/utf8"

	"strata/internal/safepath"
	"strata/internal/sheet"
	"strata/internal/userr"
)

// Preview is what a grid card shows instead of an icon.
type Preview struct {
	Type   string     `json:"type"` // text, table, doc, slides
	Lines  []string   `json:"lines,omitempty"`
	Rows   [][]string `json:"rows,omitempty"`
	Blocks []Block    `json:"blocks,omitempty"`
	Slides int        `json:"slides,omitempty"`
}

// Block is a paragraph of a document or slide.
type Block struct {
	Text    string `json:"text"`
	Heading bool   `json:"heading,omitempty"`
}

// ErrUnsupported means the grid should show the file's icon.
var ErrUnsupported = userr.Error("no preview for this file")

const (
	maxFile   = 50 << 20 // documents larger than this keep their icon
	maxPart   = 8 << 20  // inflated bytes read from one zip part
	textBytes = 4 << 10
	maxLines  = 30
	lineChars = 160
	maxBlocks = 24
	maxChars  = 1600
)

var textExt = map[string]bool{
	".txt": true, ".md": true, ".log": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".ini": true, ".conf": true, ".sh": true, ".go": true, ".js": true, ".ts": true, ".tsx": true,
	".jsx": true, ".py": true, ".java": true, ".kt": true, ".rs": true, ".c": true, ".h": true,
	".cpp": true, ".sql": true, ".html": true, ".css": true, ".xml": true,
}

// Supported reports whether name has a format Make understands.
func Supported(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".csv", ".tsv", ".xlsx", ".xlsm", ".docx", ".odt", ".pptx":
		return true
	}
	return textExt[ext]
}

// Make builds the preview for rel.
func Make(root *safepath.Root, rel string) (*Preview, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	if !Supported(rel) {
		return nil, ErrUnsupported
	}
	f, err := root.FS().Open(rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxFile {
		return nil, ErrUnsupported
	}

	switch ext := strings.ToLower(path.Ext(rel)); ext {
	case ".xlsx", ".xlsm":
		rows, err := sheet.Peek(root, rel, 12, 6)
		if err != nil {
			return nil, ErrUnsupported
		}
		return &Preview{Type: "table", Rows: rows}, nil
	case ".csv", ".tsv":
		return table(f, ext == ".tsv")
	case ".docx", ".odt", ".pptx":
		zr, err := zip.NewReader(f, fi.Size())
		if err != nil {
			return nil, ErrUnsupported
		}
		switch ext {
		case ".docx":
			return doc(zr, "word/document.xml", "p", "t", "pStyle")
		case ".odt":
			return doc(zr, "content.xml", "p", "", "")
		default:
			return slides(zr)
		}
	default:
		return text(f)
	}
}

func head(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, textBytes))
	if err != nil {
		return nil, err
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return nil, ErrUnsupported // binary under a text name
	}
	return b, nil
}

func text(r io.Reader) (*Preview, error) {
	b, err := head(r)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ToValidUTF8(string(b), "�"), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i, l := range lines {
		lines[i] = clip(strings.TrimRight(strings.ReplaceAll(l, "\t", "    "), "\r"), lineChars)
	}
	return &Preview{Type: "text", Lines: lines}, nil
}

func table(r io.Reader, tabs bool) (*Preview, error) {
	b, err := head(r)
	if err != nil {
		return nil, err
	}
	cr := csv.NewReader(bytes.NewReader(b))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	if tabs {
		cr.Comma = '\t'
	}
	var rows [][]string
	for len(rows) < 12 {
		rec, err := cr.Read()
		if err != nil {
			break // EOF, or a row cut off by the 4 KB read
		}
		if len(rec) > 6 {
			rec = rec[:6]
		}
		for i := range rec {
			rec[i] = clip(rec[i], 40)
		}
		rows = append(rows, rec)
	}
	return &Preview{Type: "table", Rows: rows}, nil
}

// part opens one member of a zip, refusing anything that inflates too far.
func part(zr *zip.Reader, name string) (io.Reader, func(), error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > maxPart {
			return nil, nil, ErrUnsupported
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, ErrUnsupported
		}
		return io.LimitReader(rc, maxPart), func() { rc.Close() }, nil
	}
	return nil, nil, ErrUnsupported
}

// doc reads paragraphs from a WordprocessingML or OpenDocument body. Text
// is collected from textTag elements, or from all character data when
// textTag is empty (ODF has no wrapper element).
func doc(zr *zip.Reader, name, paraTag, textTag, styleTag string) (*Preview, error) {
	r, done, err := part(zr, name)
	if err != nil {
		return nil, err
	}
	defer done()
	blocks, err := paragraphs(xml.NewDecoder(r), paraTag, textTag, styleTag, "h")
	if err != nil {
		return nil, err
	}
	return &Preview{Type: "doc", Blocks: blocks}, nil
}

func slides(zr *zip.Reader) (*Preview, error) {
	n := 0
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			n++
		}
	}
	r, done, err := part(zr, "ppt/slides/slide1.xml")
	if err != nil {
		return nil, err
	}
	defer done()
	blocks, err := paragraphs(xml.NewDecoder(r), "p", "t", "", "")
	if err != nil {
		return nil, err
	}
	if len(blocks) > 0 {
		blocks[0].Heading = true
	}
	return &Preview{Type: "slides", Blocks: blocks, Slides: n}, nil
}

// paragraphs walks XML tokens and returns non-empty paragraphs, matching
// element local names so namespace prefixes don't matter.
func paragraphs(d *xml.Decoder, paraTag, textTag, styleTag, headTag string) ([]Block, error) {
	d.Strict = false
	var (
		out   []Block
		cur   strings.Builder
		head  bool
		inTxt bool
		chars int
	)
	flush := func() {
		t := strings.Join(strings.Fields(cur.String()), " ")
		if t != "" {
			t = clip(t, maxChars-chars)
			chars += len(t)
			out = append(out, Block{Text: t, Heading: head})
		}
		cur.Reset()
		head = false
	}
	for len(out) < maxBlocks && chars < maxChars {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(out) > 0 {
				break // keep what parsed before the damage
			}
			return nil, ErrUnsupported
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case paraTag, headTag:
				flush()
				head = t.Name.Local == headTag && headTag != paraTag
			case textTag:
				inTxt = true
			case "tab":
				cur.WriteByte(' ')
			case styleTag:
				for _, a := range t.Attr {
					v := strings.ToLower(a.Value)
					if a.Name.Local == "val" && (strings.HasPrefix(v, "heading") || v == "title") {
						head = true
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case paraTag, headTag:
				flush()
			case textTag:
				inTxt = false
			}
		case xml.CharData:
			if textTag == "" || inTxt {
				cur.Write(t)
			}
		}
	}
	flush()
	return out, nil
}

func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// Cache keeps recent previews in memory. Keys include size and mtime, so an
// edited file is read again.
type Cache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
	limit int
	slots chan struct{}
}

type cached struct {
	key string
	p   *Preview
	err error
}

// NewCache holds up to limit previews and builds two at a time.
func NewCache(limit int) *Cache {
	return &Cache{order: list.New(), items: map[string]*list.Element{}, limit: limit, slots: make(chan struct{}, 2)}
}

// Get returns rel's preview, making it on a miss.
func (c *Cache) Get(root *safepath.Root, rel string) (*Preview, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, err
	}
	fi, err := root.FS().Stat(rel)
	if err != nil {
		return nil, safepath.Err(err)
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", rel, fi.Size(), fi.ModTime().UnixNano())

	c.mu.Lock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		v := el.Value.(*cached)
		c.mu.Unlock()
		return v.p, v.err
	}
	c.mu.Unlock()

	c.slots <- struct{}{}
	p, err := Make(root, rel)
	<-c.slots
	if err != nil && err != ErrUnsupported {
		return nil, err // don't cache transient failures
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = c.order.PushFront(&cached{key, p, err})
	for c.order.Len() > c.limit {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.items, old.Value.(*cached).key)
	}
	return p, err
}
