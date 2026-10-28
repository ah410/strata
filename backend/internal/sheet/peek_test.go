package sheet

import (
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"

	"strata/internal/safepath"
)

func TestPeekReadsTopLeftCorner(t *testing.T) {
	dir := t.TempDir()
	f := excelize.NewFile()
	for r := 1; r <= 30; r++ {
		for c := 1; c <= 12; c++ {
			ref, _ := excelize.CoordinatesToCellName(c, r)
			f.SetCellValue("Sheet1", ref, ref)
		}
	}
	if err := f.SaveAs(filepath.Join(dir, "b.xlsx")); err != nil {
		t.Fatal(err)
	}
	root, _ := safepath.NewRoot(dir)
	rows, err := Peek(root, "b.xlsx", 8, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8 || len(rows[0]) != 5 || rows[0][0] != "A1" || rows[7][4] != "E8" {
		t.Fatalf("rows = %v", rows)
	}
	if _, err := Peek(root, "missing.xlsx", 8, 5); err == nil {
		t.Fatal("missing workbook peeked")
	}
}
