package app

// mouse_test.go — Unit tests for mouse handling logic.
// These test the Model methods directly rather than via teatest because
// bubblezone coordinate resolution requires a rendered terminal.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestHandleScrollDown verifies mouse wheel down moves cursor forward.
func TestHandleScrollDown(t *testing.T) {
	m := makeStatusModel(false)
	m.tabs[0].Table.SetHeight(20)

	m.handleScroll(1)

	cursor := m.tabs[0].Table.Cursor()
	if cursor != 1 {
		t.Errorf("cursor = %d, want 1 after scroll down", cursor)
	}
}

// TestHandleScrollUp verifies mouse wheel up moves cursor backward.
func TestHandleScrollUp(t *testing.T) {
	m := makeStatusModel(false)
	m.tabs[0].Table.SetHeight(20)
	m.tabs[0].Table.SetCursor(1)

	m.handleScroll(-1)

	cursor := m.tabs[0].Table.Cursor()
	if cursor != 0 {
		t.Errorf("cursor = %d, want 0 after scroll up", cursor)
	}
}

// TestHandleScrollClampTop verifies scroll up clamps at row 0.
func TestHandleScrollClampTop(t *testing.T) {
	m := makeStatusModel(false)
	m.tabs[0].Table.SetHeight(20)

	m.handleScroll(-1) // already at 0

	cursor := m.tabs[0].Table.Cursor()
	if cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped at top)", cursor)
	}
}

// TestHandleScrollClampBottom verifies scroll down clamps at last row.
func TestHandleScrollClampBottom(t *testing.T) {
	m := makeStatusModel(false) // 2 rows: alice, bob
	m.tabs[0].Table.SetHeight(20)
	m.tabs[0].Table.SetCursor(1) // at last row

	m.handleScroll(1)

	cursor := m.tabs[0].Table.Cursor()
	if cursor != 1 {
		t.Errorf("cursor = %d, want 1 (clamped at bottom)", cursor)
	}
}

// TestHandleScrollEmptyTable verifies scroll on empty table is a no-op.
func TestHandleScrollEmptyTable(t *testing.T) {
	m := minimalModel()
	tab := makeTab([]string{"id"}, nil)
	tab.Table.SetHeight(20)
	m.tabs = []Tab{*tab}

	// Should not panic.
	m.handleScroll(1)
	m.handleScroll(-1)
}

// TestHandleScrollNoTabs verifies scroll with no tabs is a no-op.
func TestHandleScrollNoTabs(t *testing.T) {
	m := minimalModel()
	// Should not panic.
	m.handleScroll(1)
}

// TestMouseWheelDown verifies the full Update path for mouse wheel down.
func TestMouseWheelDown(t *testing.T) {
	m := makeStatusModel(false)
	m.tabs[0].Table.SetHeight(20)

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	fm := updated.(*Model)

	cursor := fm.tabs[0].Table.Cursor()
	if cursor != 1 {
		t.Errorf("cursor = %d, want 1 after wheel down", cursor)
	}
}

// TestMouseWheelUp verifies the full Update path for mouse wheel up.
func TestMouseWheelUp(t *testing.T) {
	m := makeStatusModel(false)
	m.tabs[0].Table.SetHeight(20)
	m.tabs[0].Table.SetCursor(1)

	updated, _ := m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	fm := updated.(*Model)

	cursor := fm.tabs[0].Table.Cursor()
	if cursor != 0 {
		t.Errorf("cursor = %d, want 0 after wheel up", cursor)
	}
}

// TestClickDismissesOverlay verifies left-click outside overlay closes it.
func TestClickDismissesOverlay(t *testing.T) {
	m := makeStatusModel(false)
	m.width = 100
	m.height = 30
	m.helpVisible = true

	// Click somewhere — since no zone matches the overlay, it should dismiss.
	updated, _ := m.Update(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	fm := updated.(*Model)

	if fm.helpVisible {
		t.Error("help overlay should be dismissed after click outside")
	}
}

// TestClickDismissesCellEditor verifies left-click outside closes cell editor.
func TestClickDismissesCellEditor(t *testing.T) {
	m := makeStatusModel(false)
	m.width = 100
	m.height = 30
	m.mode = modeEdit
	// Simulate a cell editor state (we only need it non-nil).
	m.cellEditor = &cellEditorState{Title: "test"}

	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	fm := updated.(*Model)

	if fm.cellEditor != nil {
		t.Error("cell editor should be dismissed after click outside")
	}
}

// TestClickDismissesTableList verifies left-click outside closes table list.
func TestClickDismissesTableList(t *testing.T) {
	m := makeStatusModel(false)
	m.width = 100
	m.height = 30
	m.tableList = &tableListState{
		Tables: []tableListEntry{{Name: "users", Rows: 2}},
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	fm := updated.(*Model)

	if fm.tableList != nil {
		t.Error("table list should be dismissed after click outside")
	}
}
