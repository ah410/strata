// Package sheet reads and writes .xlsx workbooks for the browser editor.
//
// The guiding constraint is that formulas must survive a round trip. The
// editor sends literal cell values only; formula cells are returned with both
// their formula text and a freshly computed result, and are never overwritten
// unless the user explicitly types a new formula.
package sheet

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"strata/internal/fileops"
	"strata/internal/safepath"
	"strata/internal/userr"
)

// Guard rails. A NAS will eventually be asked to open a 200k-row export, and
// shipping that to a browser helps nobody.
const (
	MaxRows = 2000
	MaxCols = 64
)

// Cell is one addressable cell.
type Cell struct {
	Ref     string `json:"ref"`               // "B7"
	Col     int    `json:"col"`               // 1-based
	Row     int    `json:"row"`               // 1-based
	Value   string `json:"value"`             // displayed result
	Formula string `json:"formula,omitempty"` // without leading '='
	Style   *Style `json:"style,omitempty"`
}

// Style carries the subset of formatting the editor renders.
type Style struct {
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Size      int    `json:"size,omitempty"`
	Color     string `json:"color,omitempty"`  // "#RRGGBB"
	Fill      string `json:"fill,omitempty"`   // "#RRGGBB"
	Align     string `json:"align,omitempty"`  // left|center|right
	Family    string `json:"family,omitempty"` // font name
	WrapText  bool   `json:"wrap,omitempty"`
	NumberFmt string `json:"numFmt,omitempty"`
}

// Merge is a merged cell region, expressed in 1-based inclusive coordinates.
type Merge struct {
	Ref     string `json:"ref"`
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	RowSpan int    `json:"rowSpan"`
	ColSpan int    `json:"colSpan"`
}

// Column carries per-column display width.
type Column struct {
	Index int     `json:"index"`
	Width float64 `json:"width"`
}

// Row carries per-row display height.
type Row struct {
	Index  int     `json:"index"`
	Height float64 `json:"height"`
}

// Grid is one worksheet rendered for the editor.
type Grid struct {
	Sheets    []string `json:"sheets"`
	Active    string   `json:"active"`
	Cells     []Cell   `json:"cells"`
	Merges    []Merge  `json:"merges"`
	Columns   []Column `json:"columns"`
	Rows      []Row    `json:"rows"`
	MaxRow    int      `json:"maxRow"`
	MaxCol    int      `json:"maxCol"`
	Truncated bool     `json:"truncated"`
	// Version identifies the file as read; a save must send it back.
	Version string `json:"version"`
}

// MaxFileSize caps workbooks before parsing. Files are read into memory and
// xlsx is a zip, so an unbounded file is an easy way to exhaust the NAS.
const MaxFileSize = 64 << 20

// ErrConflict means the file changed on disk after the editor loaded it.
var ErrConflict = userr.Error("this file changed since you opened it; reload to see the new version")

// Read opens path and renders one worksheet. An empty active name selects the
// workbook's first sheet.
func Read(root *safepath.Root, rel, active string) (*Grid, error) {
	f, version, err := open(root, rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	g, err := render(f, active)
	if err != nil {
		return nil, err
	}
	g.Version = version
	return g, nil
}

// open reads a workbook through the root, bounded in size, and reports the
// version of the bytes it read.
func open(root *safepath.Root, rel string) (*excelize.File, string, error) {
	rel, err := safepath.User(rel)
	if err != nil {
		return nil, "", err
	}
	fh, err := root.FS().Open(rel)
	if err != nil {
		return nil, "", safepath.Err(err)
	}
	defer fh.Close()

	info, err := fh.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", userr.Error("not a workbook")
	}
	if info.Size() > MaxFileSize {
		return nil, "", userr.Error(fmt.Sprintf("workbook is too large to open here (limit %d MB)", MaxFileSize>>20))
	}
	f, err := excelize.OpenReader(fh, excelize.Options{UnzipSizeLimit: 512 << 20})
	if err != nil {
		return nil, "", err
	}
	return f, fmt.Sprintf("%x-%x", info.ModTime().UnixNano(), info.Size()), nil
}

func render(f *excelize.File, active string) (*Grid, error) {
	names := f.GetSheetList()
	if len(names) == 0 {
		return nil, userr.Error("workbook has no sheets")
	}
	if active == "" || !contains(names, active) {
		active = names[0]
	}

	rows, err := f.GetRows(active)
	if err != nil {
		return nil, err
	}

	grid := &Grid{
		Sheets: names,
		Active: active,
		Cells:  make([]Cell, 0, 512),
	}

	nRows := len(rows)
	if nRows > MaxRows {
		nRows = MaxRows
		grid.Truncated = true
	}

	for r := 0; r < nRows; r++ {
		nCols := len(rows[r])
		if nCols > MaxCols {
			nCols = MaxCols
			grid.Truncated = true
		}
		for c := 0; c < nCols; c++ {
			ref, err := excelize.CoordinatesToCellName(c+1, r+1)
			if err != nil {
				continue
			}

			formula, _ := f.GetCellFormula(active, ref)
			value := rows[r][c]

			// A formula cell's cached value can be stale — openpyxl writes
			// none at all. Compute it so the browser shows a real number.
			if formula != "" {
				if computed, err := f.CalcCellValue(active, ref); err == nil && computed != "" {
					value = computed
				}
			}

			style := readStyle(f, active, ref)

			// Skip cells that carry nothing at all; they bloat the payload
			// and the editor renders empties from the grid dimensions.
			if value == "" && formula == "" && style == nil {
				continue
			}

			grid.Cells = append(grid.Cells, Cell{
				Ref:     ref,
				Col:     c + 1,
				Row:     r + 1,
				Value:   value,
				Formula: strings.TrimPrefix(formula, "="),
				Style:   style,
			})

			if r+1 > grid.MaxRow {
				grid.MaxRow = r + 1
			}
			if c+1 > grid.MaxCol {
				grid.MaxCol = c + 1
			}
		}
	}

	// Always give the editor some room to work in.
	if grid.MaxRow < 30 {
		grid.MaxRow = 30
	}
	if grid.MaxCol < 12 {
		grid.MaxCol = 12
	}

	grid.Merges = readMerges(f, active)
	grid.Columns = readColumns(f, active, grid.MaxCol)
	grid.Rows = readRows(f, active, grid.MaxRow)

	return grid, nil
}

func readStyle(f *excelize.File, sheet, ref string) *Style {
	id, err := f.GetCellStyle(sheet, ref)
	if err != nil || id == 0 {
		return nil
	}
	st, err := f.GetStyle(id)
	if err != nil || st == nil {
		return nil
	}

	out := &Style{}
	hit := false

	if st.Font != nil {
		if st.Font.Bold {
			out.Bold, hit = true, true
		}
		if st.Font.Italic {
			out.Italic, hit = true, true
		}
		if st.Font.Size > 0 {
			out.Size, hit = int(st.Font.Size), true
		}
		if c := normalizeColor(st.Font.Color); c != "" {
			out.Color, hit = c, true
		}
		if st.Font.Family != "" {
			out.Family, hit = st.Font.Family, true
		}
	}

	// Pattern fills carry the background in Color[0].
	if st.Fill.Type == "pattern" && st.Fill.Pattern > 0 && len(st.Fill.Color) > 0 {
		if c := normalizeColor(st.Fill.Color[0]); c != "" {
			out.Fill, hit = c, true
		}
	}

	if st.Alignment != nil {
		if st.Alignment.Horizontal != "" {
			out.Align, hit = st.Alignment.Horizontal, true
		}
		if st.Alignment.WrapText {
			out.WrapText, hit = true, true
		}
	}

	if st.CustomNumFmt != nil && *st.CustomNumFmt != "" {
		out.NumberFmt, hit = *st.CustomNumFmt, true
	}

	if !hit {
		return nil
	}
	return out
}

// normalizeColor turns excelize's ARGB/RGB hex into "#RRGGBB".
func normalizeColor(c string) string {
	c = strings.TrimPrefix(c, "#")
	switch len(c) {
	case 8: // AARRGGBB
		c = c[2:]
	case 6:
	default:
		return ""
	}
	if c == "000000" {
		// Excel writes an explicit black for a great many default cells;
		// treating it as "unset" keeps the dark theme readable.
		return ""
	}
	return "#" + strings.ToUpper(c)
}

func readMerges(f *excelize.File, sheet string) []Merge {
	mcs, err := f.GetMergeCells(sheet)
	if err != nil {
		return nil
	}
	out := make([]Merge, 0, len(mcs))
	for _, mc := range mcs {
		sc, sr, err := excelize.CellNameToCoordinates(mc.GetStartAxis())
		if err != nil {
			continue
		}
		ec, er, err := excelize.CellNameToCoordinates(mc.GetEndAxis())
		if err != nil {
			continue
		}
		out = append(out, Merge{
			Ref:     mc.GetStartAxis(),
			Row:     sr,
			Col:     sc,
			RowSpan: er - sr + 1,
			ColSpan: ec - sc + 1,
		})
	}
	return out
}

func readColumns(f *excelize.File, sheet string, maxCol int) []Column {
	out := make([]Column, 0, maxCol)
	for c := 1; c <= maxCol; c++ {
		name, err := excelize.ColumnNumberToName(c)
		if err != nil {
			continue
		}
		w, err := f.GetColWidth(sheet, name)
		if err != nil {
			w = 10
		}
		out = append(out, Column{Index: c, Width: w})
	}
	return out
}

func readRows(f *excelize.File, sheet string, maxRow int) []Row {
	out := make([]Row, 0, maxRow)
	for r := 1; r <= maxRow; r++ {
		h, err := f.GetRowHeight(sheet, r)
		if err != nil {
			h = 15
		}
		out = append(out, Row{Index: r, Height: h})
	}
	return out
}

// Edit is one cell change from the editor.
type Edit struct {
	Ref   string `json:"ref"`
	Value string `json:"value"`
}

// Write applies edits to path in place and returns the recomputed grid.
//
// A value beginning with '=' is stored as a formula; everything else is
// stored as a literal, with numbers coerced so Excel treats them as numeric.
// Formula cells the user did not touch are left exactly as they were.
func Write(root *safepath.Root, rel, sheetName, version string, edits []Edit) (*Grid, error) {
	f, current, err := open(root, rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Someone else (another tab, Excel over SMB) saved since this editor
	// loaded the file. Overwriting would silently discard their changes.
	if version != current {
		return nil, ErrConflict
	}

	if !contains(f.GetSheetList(), sheetName) {
		return nil, userr.Error(fmt.Sprintf("no such sheet: %s", sheetName))
	}

	for _, e := range edits {
		if _, _, err := excelize.CellNameToCoordinates(e.Ref); err != nil {
			return nil, userr.Error(fmt.Sprintf("bad cell reference %q", e.Ref))
		}

		if strings.HasPrefix(e.Value, "=") {
			if err := f.SetCellFormula(sheetName, e.Ref, e.Value); err != nil {
				return nil, err
			}
			continue
		}

		// Replacing a formula with a literal means clearing the formula
		// first, otherwise excelize keeps both and Excel prefers the formula.
		if existing, _ := f.GetCellFormula(sheetName, e.Ref); existing != "" {
			if err := f.SetCellFormula(sheetName, e.Ref, ""); err != nil {
				return nil, err
			}
		}

		if err := setTyped(f, sheetName, e.Ref, e.Value); err != nil {
			return nil, err
		}
	}

	// Write a complete new file and rename it over the old one, so a crash
	// mid-save can never leave a half-written workbook.
	if err := fileops.WriteAtomic(root, rel, func(w io.Writer) error { return f.Write(w) }); err != nil {
		return nil, err
	}

	// Re-read so the response carries freshly calculated formula results.
	return Read(root, rel, sheetName)
}

// setTyped writes v with its natural Excel type so that numeric cells stay
// numeric — a number stored as text breaks every formula referencing it.
func setTyped(f *excelize.File, sheet, ref, v string) error {
	if v == "" {
		return f.SetCellValue(sheet, ref, nil)
	}

	// Strip thousands separators and currency before testing numeric-ness,
	// so a user typing "192,556" gets a number rather than a string.
	probe := strings.NewReplacer(",", "", "$", "", " ", "").Replace(v)
	if n, err := parseNumber(probe); err == nil {
		return f.SetCellValue(sheet, ref, n)
	}

	switch strings.ToLower(v) {
	case "true":
		return f.SetCellValue(sheet, ref, true)
	case "false":
		return f.SetCellValue(sheet, ref, false)
	}

	return f.SetCellValue(sheet, ref, v)
}

func parseNumber(s string) (float64, error) {
	var n float64
	// Reject strings Sscanf would partially consume, e.g. "12abc".
	if _, err := fmt.Sscanf(s, "%g", &n); err != nil {
		return 0, err
	}
	if fmt.Sprintf("%g", n) != s {
		// Allow values that round-trip differently but are still clean
		// numbers, such as "192556.0" or "0012".
		var check float64
		if _, err := fmt.Sscanf(s, "%g", &check); err != nil || check != n {
			return 0, fmt.Errorf("not a number")
		}
		for _, r := range s {
			if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
				return 0, fmt.Errorf("not a number")
			}
		}
	}
	return n, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Peek returns the top-left values of the first sheet, for a preview card.
// It streams rows instead of loading the whole sheet.
func Peek(root *safepath.Root, rel string, maxRows, maxCols int) ([][]string, error) {
	f, _, err := open(root, rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names := f.GetSheetList()
	if len(names) == 0 {
		return nil, userr.Error("workbook has no sheets")
	}
	rows, err := f.Rows(names[0])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]string{}
	for len(out) < maxRows && rows.Next() {
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		if len(cols) > maxCols {
			cols = cols[:maxCols]
		}
		out = append(out, cols)
	}
	return out, nil
}
