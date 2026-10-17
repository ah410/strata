package sheet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"strata/internal/safepath"
)

// newBook writes a small workbook with a formula and returns its root.
func newBook(t *testing.T) (*safepath.Root, string) {
	t.Helper()
	dir := t.TempDir()
	f := excelize.NewFile()
	f.SetCellValue("Sheet1", "A1", 2)
	f.SetCellValue("Sheet1", "A2", 3)
	f.SetCellFormula("Sheet1", "A3", "SUM(A1:A2)")
	if err := f.SaveAs(filepath.Join(dir, "book.xlsx")); err != nil {
		t.Fatal(err)
	}
	root, err := safepath.NewRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func TestWriteRecalculatesAndLeavesNoTempFiles(t *testing.T) {
	root, dir := newBook(t)
	g, err := Read(root, "book.xlsx", "")
	if err != nil {
		t.Fatal(err)
	}
	if c := cellAt(g, "A3"); c == nil || c.Value != "5" || c.Formula != "SUM(A1:A2)" {
		t.Fatalf("A3 before = %+v", c)
	}

	g2, err := Write(root, "book.xlsx", "Sheet1", g.Version, []Edit{{Ref: "A1", Value: "10"}})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if c := cellAt(g2, "A3"); c == nil || c.Value != "13" {
		t.Errorf("A3 after = %+v, want 13", c)
	}
	if g2.Version == g.Version {
		t.Error("version did not change after save")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".strata-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestStaleVersionIsRejected(t *testing.T) {
	root, dir := newBook(t)
	g, err := Read(root, "book.xlsx", "")
	if err != nil {
		t.Fatal(err)
	}

	// Another editor saves first.
	time.Sleep(10 * time.Millisecond)
	if _, err := Write(root, "book.xlsx", "Sheet1", g.Version, []Edit{{Ref: "A1", Value: "7"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "book.xlsx"))

	// The first editor, still holding the old version, must not clobber it.
	_, err = Write(root, "book.xlsx", "Sheet1", g.Version, []Edit{{Ref: "A2", Value: "99"}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: err = %v, want ErrConflict", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "book.xlsx"))
	if string(before) != string(after) {
		t.Error("file changed despite the conflict")
	}

	if _, err := Write(root, "book.xlsx", "Sheet1", "", []Edit{{Ref: "A2", Value: "1"}}); !errors.Is(err, ErrConflict) {
		t.Errorf("missing version: err = %v, want ErrConflict", err)
	}
}

func TestOversizedWorkbookIsRefused(t *testing.T) {
	root, dir := newBook(t)
	big := filepath.Join(dir, "big.xlsx")
	if err := os.WriteFile(big, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, MaxFileSize+1); err != nil {
		t.Fatal(err)
	}
	_, err := Read(root, "big.xlsx", "")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("oversized workbook: err = %v", err)
	}
}

func TestWorkbookOutsideRootIsBlocked(t *testing.T) {
	root, dir := newBook(t)
	outside := t.TempDir()
	data, _ := os.ReadFile(filepath.Join(dir, "book.xlsx"))
	os.WriteFile(filepath.Join(outside, "theirs.xlsx"), data, 0o644)
	if err := os.Symlink(filepath.Join(outside, "theirs.xlsx"), filepath.Join(dir, "link.xlsx")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Read(root, "link.xlsx", ""); !errors.Is(err, safepath.ErrEscape) {
		t.Errorf("read through escaping symlink: err = %v, want ErrEscape", err)
	}
}
