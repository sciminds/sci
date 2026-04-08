package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestEditModeEnter verifies 'i' enters edit mode.
func TestTeatestEditModeEnter(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "i")

	fm := finalModel(t, tm)

	if fm.mode != modeEdit {
		t.Errorf("mode = %d, want modeEdit (%d)", fm.mode, modeEdit)
	}
}

// TestTeatestEditModeBlockedReadOnly verifies 'i' is blocked on read-only tables.
func TestTeatestEditModeBlockedReadOnly(t *testing.T) {
	m := newReadOnlyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)

	sendKey(tm, "i")

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal on read-only table", fm.mode)
	}
}

// TestTeatestCellEditorSave verifies editing a cell and saving.
func TestTeatestCellEditorSave(t *testing.T) {
	tm, store := startTeatest(t)

	// Enter edit mode, open cell editor.
	sendKey(tm, "i")
	sendSpecial(tm, tea.KeyEnter)

	// Clear existing text and type new value.
	tm.Send(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	tm.Send(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	tm.Type("Gizmo")
	// Save with Enter.
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)

	if fm.cellEditor != nil {
		t.Error("cell editor should be closed after save")
	}

	// Verify DB was updated. Tab 0 is "products", col 1 is "title".
	_, rows, _, _, err := store.QueryTable("products")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows returned")
	}
	// Row 0, col 1 (title) should be "Gizmo".
	if rows[0][1] != "Gizmo" {
		t.Errorf("cell value = %q, want %q", rows[0][1], "Gizmo")
	}
}

// TestTeatestCellEditorCancel verifies Esc cancels without saving.
func TestTeatestCellEditorCancel(t *testing.T) {
	tm, store := startTeatest(t)

	sendKey(tm, "i")
	sendSpecial(tm, tea.KeyEnter)
	tm.Type("SHOULD NOT SAVE")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.cellEditor != nil {
		t.Error("cell editor should be closed after cancel")
	}

	// Verify DB was NOT updated.
	_, rows, _, _, err := store.QueryTable("products")
	if err != nil {
		t.Fatalf("QueryTable: %v", err)
	}
	if rows[0][1] != "Widget" {
		t.Errorf("cell value = %q, want %q (unchanged)", rows[0][1], "Widget")
	}
}

// TestTeatestEditModeExit verifies Esc exits edit mode to normal.
func TestTeatestEditModeExit(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "i")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after Esc", fm.mode)
	}
}
