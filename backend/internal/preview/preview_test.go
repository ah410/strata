package preview

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"strata/internal/safepath"
)

func setup(t *testing.T) (*safepath.Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func put(t *testing.T, dir, name string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func texts(b []Block) string {
	var s []string
	for _, x := range b {
		if x.Heading {
			s = append(s, "#"+x.Text)
		} else {
			s = append(s, x.Text)
		}
	}
	return strings.Join(s, "|")
}

func TestText(t *testing.T) {
	root, dir := setup(t)
	put(t, dir, "a.md", []byte("# Title\n\tindented\r\n"+strings.Repeat("x", 500)+"\n"+strings.Repeat("line\n", 100)))
	p, err := Make(root, "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "text" || p.Lines[0] != "# Title" || p.Lines[1] != "    indented" || len(p.Lines) != maxLines {
		t.Fatalf("lines = %q", p.Lines[:3])
	}
	if !strings.HasSuffix(p.Lines[2], "…") || len(p.Lines[2]) > lineChars+4 {
		t.Fatalf("long line not clipped: %d", len(p.Lines[2]))
	}
	put(t, dir, "bin.txt", []byte("ab\x00cd"))
	if _, err := Make(root, "bin.txt"); err != ErrUnsupported {
		t.Fatalf("binary text file: %v", err)
	}
}

func TestCSV(t *testing.T) {
	root, dir := setup(t)
	put(t, dir, "t.csv", []byte("name,qty,\"note, with comma\"\nbolts,4,x\n"))
	p, err := Make(root, "t.csv")
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != "table" || len(p.Rows) != 2 || p.Rows[0][2] != "note, with comma" {
		t.Fatalf("rows = %q", p.Rows)
	}
}

func TestDocx(t *testing.T) {
	root, dir := setup(t)
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quarterly</w:t></w:r><w:r><w:t xml:space="preserve"> report</w:t></w:r></w:p>
<w:p><w:r><w:t>First</w:t><w:tab/><w:t>para.</w:t></w:r></w:p>
<w:p></w:p>
<w:p><w:r><w:instrText>PAGE</w:instrText><w:t>Second.</w:t></w:r></w:p>
</w:body></w:document>`
	put(t, dir, "r.docx", zipOf(t, map[string]string{"word/document.xml": body}))
	p, err := Make(root, "r.docx")
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(p.Blocks); p.Type != "doc" || got != "#Quarterly report|First para.|Second." {
		t.Fatalf("blocks = %q", got)
	}
}

func TestOdt(t *testing.T) {
	root, dir := setup(t)
	body := `<office:document-content xmlns:office="o" xmlns:text="t"><office:body><office:text>
<text:h text:outline-level="1">Minutes</text:h><text:p>Met at <text:span>noon</text:span>.</text:p>
</office:text></office:body></office:document-content>`
	put(t, dir, "m.odt", zipOf(t, map[string]string{"content.xml": body}))
	p, err := Make(root, "m.odt")
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(p.Blocks); got != "#Minutes|Met at noon." {
		t.Fatalf("blocks = %q", got)
	}
}

func TestPptx(t *testing.T) {
	root, dir := setup(t)
	slide := `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree>
<p:sp><p:txBody><a:p><a:r><a:t>Roadmap</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:txBody><a:p><a:r><a:t>Ship 1.1</a:t></a:r></a:p><a:p><a:r><a:t>Fix uploads</a:t></a:r></a:p></p:txBody></p:sp>
</p:spTree></p:cSld></p:sld>`
	put(t, dir, "d.pptx", zipOf(t, map[string]string{"ppt/slides/slide1.xml": slide, "ppt/slides/slide2.xml": slide}))
	p, err := Make(root, "d.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(p.Blocks); p.Type != "slides" || p.Slides != 2 || got != "#Roadmap|Ship 1.1|Fix uploads" {
		t.Fatalf("slides %d blocks %q", p.Slides, got)
	}
}

func TestHostileDocuments(t *testing.T) {
	root, dir := setup(t)
	// A part that claims to inflate beyond the cap is refused unread.
	big := strings.Repeat("<w:p><w:t>a</w:t></w:p>", maxPart/20)
	put(t, dir, "bomb.docx", zipOf(t, map[string]string{"word/document.xml": "<w:document>" + big + "</w:document>"}))
	if _, err := Make(root, "bomb.docx"); err != ErrUnsupported {
		t.Fatalf("oversized part: %v", err)
	}
	put(t, dir, "junk.docx", []byte("not a zip"))
	put(t, dir, "empty.pptx", zipOf(t, map[string]string{"x": "y"}))
	put(t, dir, "bad.docx", zipOf(t, map[string]string{"word/document.xml": "<<<>"}))
	for _, n := range []string{"junk.docx", "empty.pptx", "bad.docx", "photo.jpg", "../x.txt"} {
		if _, err := Make(root, n); err == nil {
			t.Errorf("%s: no error", n)
		}
	}
	// Many paragraphs: the preview stops early.
	many := strings.Repeat("<w:p><w:t>"+strings.Repeat("w ", 50)+"</w:t></w:p>", 5000)
	put(t, dir, "long.docx", zipOf(t, map[string]string{"word/document.xml": "<w:document>" + many + "</w:document>"}))
	p, err := Make(root, "long.docx")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, b := range p.Blocks {
		total += len(b.Text)
	}
	if len(p.Blocks) > maxBlocks || total > maxChars+len(p.Blocks)*4 {
		t.Fatalf("%d blocks, %d chars", len(p.Blocks), total)
	}
}

func TestCacheRereadsChangedFiles(t *testing.T) {
	root, dir := setup(t)
	c := NewCache(2)
	put(t, dir, "a.txt", []byte("one"))
	p, _ := c.Get(root, "a.txt")
	put(t, dir, "a.txt", []byte("two!"))
	q, _ := c.Get(root, "a.txt")
	if p.Lines[0] != "one" || q.Lines[0] != "two!" {
		t.Fatalf("got %q then %q", p.Lines, q.Lines)
	}
	put(t, dir, "b.txt", []byte("b"))
	put(t, dir, "c.txt", []byte("c"))
	c.Get(root, "b.txt")
	c.Get(root, "c.txt")
	if c.order.Len() != 2 {
		t.Fatalf("cache holds %d, limit 2", c.order.Len())
	}
	if _, err := c.Get(root, "missing.txt"); err == nil {
		t.Fatal("missing file previewed")
	}
}
