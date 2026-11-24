package preview

import (
	"bufio"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// Groups whose text is metadata, not document body.
var rtfSkip = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true,
	"header": true, "headerl": true, "headerr": true, "headerf": true, "footer": true,
	"footerl": true, "footerr": true, "footerf": true, "listtable": true,
	"listoverridetable": true, "rsidtbl": true, "generator": true, "xmlnstbl": true,
	"themedata": true, "colorschememapping": true, "latentstyles": true, "datastore": true,
	"object": true, "fldinst": true, "footnote": true, "annotation": true,
}

// rtf reads the first paragraphs of an RTF document's body.
func rtf(r io.Reader) (*Preview, error) {
	br := bufio.NewReader(io.LimitReader(r, maxPart))
	if b, _ := br.Peek(5); string(b) != `{\rtf` {
		return nil, ErrUnsupported
	}
	var (
		out   []Block
		cur   strings.Builder
		chars int
		depth int
		skip  = -1 // depth at which a skipped group began
		ucN   = 1  // chars to drop after \uN
		drop  int
		dec   = charmap.Windows1252.NewDecoder()
	)
	flush := func() {
		t := strings.Join(strings.Fields(cur.String()), " ")
		if t != "" {
			t = clip(t, maxChars-chars)
			chars += len(t)
			out = append(out, Block{Text: t})
		}
		cur.Reset()
	}
	emit := func(s string) {
		if skip >= 0 {
			return
		}
		if drop > 0 {
			drop--
			return
		}
		cur.WriteString(s)
	}
	for len(out) < maxBlocks && chars < maxChars {
		c, err := br.ReadByte()
		if err != nil {
			break
		}
		switch c {
		case '{':
			depth++
		case '}':
			if depth == skip {
				skip = -1
			}
			depth--
		case '\r', '\n':
		case '\\':
			word, arg, hasArg, sym := rtfControl(br)
			if sym != 0 {
				switch sym {
				case '\\', '{', '}':
					emit(string(sym))
				case '~':
					emit(" ")
				case '\n', '\r':
					if skip < 0 {
						flush()
					}
				case '*':
					if skip < 0 {
						skip = depth
					}
				case '\'':
					h := make([]byte, 2)
					if _, err := io.ReadFull(br, h); err == nil {
						if v, err := strconv.ParseUint(string(h), 16, 8); err == nil {
							s, _ := dec.String(string([]byte{byte(v)}))
							emit(s)
						}
					}
				}
				continue
			}
			switch {
			case rtfSkip[word]:
				if skip < 0 {
					skip = depth
				}
			case skip >= 0:
			case word == "par" || word == "line" || word == "sect" || word == "page" || word == "row":
				flush()
			case word == "tab" || word == "cell":
				cur.WriteByte(' ')
			case word == "uc" && hasArg:
				ucN = min(max(arg, 0), 8)
			case word == "u" && hasArg:
				// \uN is a signed 16-bit code unit; ignore anything else.
				if arg < -32768 || arg > 65535 {
					break
				}
				cur.WriteRune(rune(uint16(arg)))
				drop = ucN
			case word == "emdash", word == "endash":
				cur.WriteString("-")
			case word == "bullet":
				cur.WriteString("•")
			case word == "lquote", word == "rquote":
				cur.WriteString("'")
			case word == "ldblquote", word == "rdblquote":
				cur.WriteString(`"`)
			}
		default:
			emit(string(c))
		}
	}
	flush()
	if len(out) == 0 {
		return nil, ErrUnsupported
	}
	return &Preview{Type: "doc", Blocks: out}, nil
}

// rtfControl reads what follows a backslash: a control word with an
// optional numeric argument, or a single control symbol.
func rtfControl(br *bufio.Reader) (word string, arg int, hasArg bool, sym byte) {
	c, err := br.ReadByte()
	if err != nil {
		return "", 0, false, 0
	}
	if !isLetter(c) {
		return "", 0, false, c
	}
	var w strings.Builder
	for isLetter(c) && w.Len() < 32 {
		w.WriteByte(c)
		if c, err = br.ReadByte(); err != nil {
			return w.String(), 0, false, 0
		}
	}
	var n strings.Builder
	if c == '-' || (c >= '0' && c <= '9') {
		for (c == '-' && n.Len() == 0) || (c >= '0' && c <= '9') {
			n.WriteByte(c)
			if c, err = br.ReadByte(); err != nil || n.Len() > 10 {
				break
			}
		}
		arg, err := strconv.Atoi(n.String())
		hasArg = err == nil
		if c != ' ' {
			br.UnreadByte()
		}
		return w.String(), arg, hasArg, 0
	}
	if c != ' ' {
		br.UnreadByte() // the delimiter belongs to the text
	}
	return w.String(), 0, false, 0
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
