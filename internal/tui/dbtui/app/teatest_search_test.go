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

// TestTeatestSearchColumnScoped verifies @col:value filtering via the TUI.
func TestTeatestSearchColumnScoped(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("@title: Widget")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// Only "Widget" in the title column should match.
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 for @title: Widget", len(tab.CellRows))
	}
}

// TestTeatestSearchNegation verifies -term exclusion via the TUI.
func TestTeatestSearchNegation(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("-Widget")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// "Widget" row should be excluded, leaving 2 of 3 products.
	if len(tab.CellRows) != 2 {
		t.Errorf("filtered rows = %d, want 2 after excluding Widget", len(tab.CellRows))
	}
	for _, row := range tab.CellRows {
		for _, c := range row {
			if c.Value == "Widget" {
				t.Error("Widget row should have been excluded by negation")
			}
		}
	}
}

// TestTeatestSearchBackspace verifies backspace removes characters from query.
func TestTeatestSearchBackspace(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Widgetx")
	sendSpecial(tm, tea.KeyBackspace) // remove the 'x'

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open")
	}
	if fm.search.Query != "Widget" {
		t.Errorf("search.Query = %q, want %q after backspace", fm.search.Query, "Widget")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 for 'Widget'", len(tab.CellRows))
	}
}

// TestTeatestSearchORSyntax verifies pipe-separated OR groups filter correctly.
func TestTeatestSearchORSyntax(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("Widget | Gadget")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// "Widget" OR "Gadget" should match 2 of 3 products.
	if len(tab.CellRows) != 2 {
		t.Errorf("filtered rows = %d, want 2 for 'Widget | Gadget'", len(tab.CellRows))
	}
}

// TestTeatestSearchNoMatches verifies search with no results shows empty table.
func TestTeatestSearchNoMatches(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("zzzznonexistent")

	fm := finalModel(t, tm)

	if fm.search == nil {
		t.Fatal("search should be open")
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 0 {
		t.Errorf("filtered rows = %d, want 0 for no-match query", len(tab.CellRows))
	}
}

// TestTeatestSearchNoMatchesThenEscRestores verifies Esc after zero-match search restores all rows.
func TestTeatestSearchNoMatchesThenEscRestores(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("zzzznonexistent")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 3 {
		t.Errorf("rows = %d, want 3 (restored after Esc from zero-match search)", len(tab.CellRows))
	}
}
