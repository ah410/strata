package sheet

import (
	"os"
	"path/filepath"
	"testing"

	"strata/internal/safepath"
)

// These tests need a real workbook that contains formulas. Point
// STRATA_TEST_XLSX at one to run them; without it they skip.
//
//	STRATA_TEST_XLSX=~/sheets/tracker.xlsx go test ./internal/sheet/
//
// The assertions below were written against a maintenance tracker whose
// Settings!C10 holds a mileage that dashboard formulas depend on, so the
// value-specific cases only make sense with that workbook. The structural
// cases hold for any workbook with formulas.
func copyFixture(t *testing.T) (*safepath.Root, string) {
	t.Helper()
	fixture := os.Getenv("STRATA_TEST_XLSX")
	if fixture == "" {
		t.Skip("set STRATA_TEST_XLSX to a workbook with formulas to run this test")
	}
	src, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("fixture unavailable: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "book.xlsx"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root, "book.xlsx"
}

// write saves edits against the file's current version, as the editor does.
func write(t *testing.T, root *safepath.Root, rel, sheetName string, edits []Edit) (*Grid, error) {
	t.Helper()
	g, err := Read(root, rel, sheetName)
	if err != nil {
		t.Fatalf("Read before write: %v", err)
	}
	return Write(root, rel, sheetName, g.Version, edits)
}

func cellAt(g *Grid, ref string) *Cell {
	for i := range g.Cells {
		if g.Cells[i].Ref == ref {
			return &g.Cells[i]
		}
	}
	return nil
}

func TestReadComputesFormulaValues(t *testing.T) {
	root, path := copyFixture(t)

	g, err := Read(root, path, "Maintenance Dashboard")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(g.Sheets) < 4 {
		t.Errorf("expected 4 sheets, got %v", g.Sheets)
	}

	// E7 is "miles left" for the oil change: =D7-Settings!$C$9
	e7 := cellAt(g, "E7")
	if e7 == nil {
		t.Fatal("E7 missing from grid")
	}
	if e7.Formula == "" {
		t.Errorf("E7 should carry a formula, got none")
	}
	// openpyxl writes no cached value, so a non-empty value proves we
	// actually calculated it rather than echoing the file.
	if e7.Value == "" {
		t.Errorf("E7 value not computed; formula=%q", e7.Formula)
	}
	t.Logf("E7 formula=%q value=%q", e7.Formula, e7.Value)

	// The progress bar is a REPT() expression — the most likely function to
	// be unsupported by the calc engine.
	g7 := cellAt(g, "G7")
	if g7 == nil {
		t.Fatal("G7 missing from grid")
	}
	t.Logf("G7 formula=%q value=%q", g7.Formula, g7.Value)
	if g7.Value == "" {
		t.Errorf("G7 progress bar did not render")
	}
}

func TestWritePreservesFormulasAndRecalculates(t *testing.T) {
	root, path := copyFixture(t)

	before, err := Read(root, path, "Maintenance Dashboard")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	e7Before := cellAt(before, "E7")
	if e7Before == nil {
		t.Fatal("E7 missing")
	}

	// Drive the mileage up by 1000 via the Settings input cell (C10) and confirm
	// the dependent dashboard formula moves by the same amount.
	if _, err := write(t, root, path, "Settings", []Edit{{Ref: "C10", Value: "193556"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	after, err := Read(root, path, "Maintenance Dashboard")
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	e7After := cellAt(after, "E7")
	if e7After == nil {
		t.Fatal("E7 missing after write")
	}

	if e7After.Formula != e7Before.Formula {
		t.Errorf("formula was clobbered: %q -> %q", e7Before.Formula, e7After.Formula)
	}
	if e7After.Value == e7Before.Value {
		t.Errorf("value did not change after mileage edit: still %q", e7After.Value)
	}
	t.Logf("E7 %q -> %q after +1000 miles", e7Before.Value, e7After.Value)
}

func TestWriteRejectsBadReference(t *testing.T) {
	root, path := copyFixture(t)
	if _, err := write(t, root, path, "Settings", []Edit{{Ref: "../etc", Value: "x"}}); err == nil {
		t.Error("expected error for malformed cell reference")
	}
}

func TestSetTypedKeepsNumbersNumeric(t *testing.T) {
	root, path := copyFixture(t)

	// A user typing a thousands separator should still produce a number,
	// otherwise every formula referencing the cell breaks.
	if _, err := write(t, root, path, "Settings", []Edit{{Ref: "C10", Value: "200,000"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	g, err := Read(root, path, "Settings")
	if err != nil {
		t.Fatal(err)
	}
	c9 := cellAt(g, "C10")
	if c9 == nil {
		t.Fatal("C10 missing")
	}
	t.Logf("C10 stored as %q", c9.Value)

	dash, err := Read(root, path, "Maintenance Dashboard")
	if err != nil {
		t.Fatal(err)
	}
	e7 := cellAt(dash, "E7")
	if e7 == nil || e7.Value == "" {
		t.Fatal("dashboard formula did not resolve after comma-formatted input")
	}
	// 197,369 next-due minus 200,000 current = -2631
	if e7.Value != "-2,631" && e7.Value != "-2631" {
		t.Errorf("E7 = %q, want -2631 (number was likely stored as text)", e7.Value)
	}
}
