package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTeatestSearchIncremental verifies typing a query filters rows.
func TestTeatestSearchIncremental(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Widget")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should still be open while typing")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// Only "Widget" should match in the products table.
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1", len(tab.CellRows))
	}
}

// TestTeatestSearchEnterKeeps verifies Enter closes search but keeps filter.
func TestTeatestSearchEnterKeeps(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Gadget")
	sendSpecial(tm, tea.KeyEnter)

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search state should be preserved after Enter")
	}
	if !fm.search.Committed {
		t.Error("search should be committed after Enter")
	}
	if fm.search.Query != "Gadget" {
		t.Errorf("search.Query = %q, want %q", fm.search.Query, "Gadget")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// Rows should still be filtered to just "Gadget".
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 (filter kept after Enter)", len(tab.CellRows))
	}
}

// TestTeatestSearchEscRestores verifies Esc restores all rows.
func TestTeatestSearchEscRestores(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Widget")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.search != nil {
		t.Error("search should be closed after Esc")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// All 3 rows should be restored.
	if len(tab.CellRows) != 3 {
		t.Errorf("rows = %d, want 3 (restored after Esc)", len(tab.CellRows))
	}
}

// TestTeatestSearchNavigation verifies up/down moves cursor in filtered results.
func TestTeatestSearchNavigation(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	// Don't type anything — all rows visible.
	// Navigate down within search mode.
	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyDown})

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if got := tab.Table.Cursor(); got != 2 {
		t.Errorf("cursor = %d, want 2 after two down presses", got)
	}
}

// TestTeatestSearchReopenPreservesQuery verifies / after Enter reopens with previous query.
func TestTeatestSearchReopenPreservesQuery(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Gadget")
	sendSpecial(tm, tea.KeyEnter)

	// Reopen search bar.
	sendKey(tm, "/")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open after reopen")
	}
	if fm.search.Committed {
		t.Error("search should not be committed after reopen")
	}
	if fm.search.Query != "Gadget" {
		t.Errorf("search.Query = %q, want %q (preserved from before)", fm.search.Query, "Gadget")
	}
}
