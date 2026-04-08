package app

import (
	"os"
	"testing"

	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestVisualExtendJ verifies J extends selection downward.
func TestTeatestVisualExtendJ(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v") // enter visual
	sendKey(tm, "J") // extend down (sets anchor)
	sendKey(tm, "J") // extend down again

	fm := finalModel(t, tm)

	if fm.visual == nil {
		t.Fatal("visual state is nil")
	}
	if fm.visual.Anchor < 0 {
		t.Error("expected anchor to be set after J")
	}
	sel := fm.effectiveVisualSelection()
	if len(sel) != 3 {
		t.Errorf("selection count = %d, want 3 (anchor row + 2 extensions)", len(sel))
	}
}

// TestTeatestVisualExtendK verifies K extends selection upward.
func TestTeatestVisualExtendK(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, "j") // move down (no anchor)
	sendKey(tm, "j") // move down
	sendKey(tm, "K") // extend up (sets anchor at current, moves up)

	fm := finalModel(t, tm)

	if fm.visual == nil {
		t.Fatal("visual state is nil")
	}
	sel := fm.effectiveVisualSelection()
	if len(sel) < 2 {
		t.Errorf("selection count = %d, want >= 2", len(sel))
	}
}

// TestTeatestVisualToggle verifies space toggles individual row selection.
func TestTeatestVisualToggle(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, " ") // toggle row 0

	fm := finalModel(t, tm)

	if fm.visual == nil {
		t.Fatal("visual state is nil")
	}
	if !fm.visual.Selected[0] {
		t.Error("expected row 0 to be selected after space toggle")
	}
}

// TestTeatestVisualYank verifies y copies to internal clipboard.
func TestTeatestVisualYank(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, " ") // toggle row 0
	sendKey(tm, "y") // yank

	fm := finalModel(t, tm)

	if len(fm.clipboard) == 0 {
		t.Error("expected clipboard to be populated after yank")
	}
}

// TestTeatestVisualDelete verifies d deletes selected rows from DB.
func TestTeatestVisualDelete(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, "d") // delete cursor row (row 0)

	fm := finalModel(t, tm)
	_ = fm

	// Verify row count decreased. Tab 0 is "products" (3 rows initially).
	count, err := store.TableRowCount("products")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2 after deleting 1 row", count)
	}
}

// TestTeatestVisualCut verifies x yanks and deletes.
func TestTeatestVisualCut(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, "x") // cut cursor row

	fm := finalModel(t, tm)

	if len(fm.clipboard) == 0 {
		t.Error("expected clipboard to be populated after cut")
	}

	count, err := store.TableRowCount("products")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2 after cut", count)
	}
}

// TestTeatestVisualPasteAfter verifies p pastes after cursor.
func TestTeatestVisualPasteAfter(t *testing.T) {
	tm, store := startTeatest(t)

	// Yank first, then paste.
	sendKey(tm, "v")
	sendKey(tm, "y") // yank row 0
	sendKey(tm, "p") // paste after

	fm := finalModel(t, tm)
	_ = fm

	count, err := store.TableRowCount("products")
	if err != nil {
		t.Fatalf("TableRowCount: %v", err)
	}
	if count != 4 {
		t.Errorf("row count = %d, want 4 after pasting 1 row", count)
	}
}

// TestTeatestVisualExport verifies e exports selected rows to CSV.
func TestTeatestVisualExport(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "v")
	sendKey(tm, "e") // export

	fm := finalModel(t, tm)
	_ = fm

	// Check that the CSV file was created.
	csvPath := "products_selection.csv"
	if _, err := os.Stat(csvPath); os.IsNotExist(err) {
		t.Errorf("expected %s to be created", csvPath)
	} else {
		// Clean up.
		_ = os.Remove(csvPath)
	}
}

// TestTeatestVisualBlockedReadOnly verifies visual mode is blocked on read-only.
func TestTeatestVisualBlockedReadOnly(t *testing.T) {
	m := newReadOnlyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)

	sendKey(tm, "v")

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Error("visual mode should be blocked on read-only table")
	}
	if fm.visual != nil {
		t.Error("visual state should be nil on read-only table")
	}
}
