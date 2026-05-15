package duck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSheetsMultiSheet(t *testing.T) {
	got, err := listSheets("testdata/tiny.xlsx")
	if err != nil {
		t.Fatalf("listSheets: %v", err)
	}
	want := []string{"people", "extras"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sheet[%d] = %q, want %q (full order matters)", i, got[i], want[i])
		}
	}
}

func TestListSheetsSingleSheet(t *testing.T) {
	got, err := listSheets("testdata/single_sheet.xlsx")
	if err != nil {
		t.Fatalf("listSheets: %v", err)
	}
	if len(got) != 1 || got[0] != "only" {
		t.Errorf("got %v, want [only]", got)
	}
}

func TestListSheetsNotXLSX(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listSheets(plain); err == nil {
		t.Error("expected error for non-zip file, got nil")
	}
}

func TestListSheetsMissingWorkbookXML(t *testing.T) {
	// A valid zip with no xl/workbook.xml entry should error cleanly.
	// We reuse a fixture that is a real zip but contains no workbook.xml
	// by using a small in-memory archive.
	dir := t.TempDir()
	bogus := filepath.Join(dir, "bogus.xlsx")
	// Empty zip header bytes: PK\x05\x06 + 18 zero bytes = empty central directory.
	emptyZip := []byte{0x50, 0x4b, 0x05, 0x06,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if err := os.WriteFile(bogus, emptyZip, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listSheets(bogus); err == nil {
		t.Error("expected error when workbook.xml is missing, got nil")
	}
}
