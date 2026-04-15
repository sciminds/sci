package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	teatest "github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestSearchIncremental verifies typing a query filters rows.
func TestTeatestSearchIncremental(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestTeatestSearchMultiTokenSameCell verifies multi-word substring AND
// matches when both tokens live in the same cell. Uses the products fixture:
// "Doohickey" is the only row whose title contains both "doo" and "key".
func TestTeatestSearchMultiTokenSameCell(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("doo key")

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 for 'doo key'", len(tab.CellRows))
	}
}

// TestTeatestSearchMultiTokenCrossColumn verifies a token can live in one
// column and another token in a different column — Zotero-style
// all-fields-AND semantics. Builds a fixture where "alice" is in the name
// column and "210" is in the location column of the same row.
func TestTeatestSearchMultiTokenCrossColumn(t *testing.T) {
	t.Parallel()
	stmts := []string{
		`CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT, location TEXT)`,
		`INSERT INTO people VALUES
			(1, 'Alice', 'Building 210'),
			(2, 'Bob',   'Building 300'),
			(3, 'Carol', 'Building 210')`,
	}
	m, _ := newTeatestModelWithSchema(t, stmts)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForOutput(t, tm, "people")

	sendKey(tm, "/")
	tm.Type("alice 210") // "alice" in name, "210" in location → row 1 only

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 for 'alice 210' (cross-column AND)", len(tab.CellRows))
	}
}

// TestTeatestSearchSubstringNotFuzzy is a regression guard documenting the
// intentional switch from sahilm fuzzy to substring semantics: non-contiguous
// characters must NOT match. Under the old fuzzy logic, "wdgt" matched
// "Widget"; under the new substring logic it must not.
func TestTeatestSearchSubstringNotFuzzy(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("wdgt")

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 0 {
		t.Errorf("filtered rows = %d, want 0 — fuzzy tolerance removed on purpose", len(tab.CellRows))
	}
}

// TestTeatestSearchSubstringMiddleMatches verifies substring matches hit in
// the middle of a cell value, not just prefixes — e.g. "idget" → "Widget".
func TestTeatestSearchSubstringMiddleMatches(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "/")
	tm.Type("idget")

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	if len(tab.CellRows) != 1 {
		t.Errorf("filtered rows = %d, want 1 for middle-substring 'idget'", len(tab.CellRows))
	}
}

// TestTeatestSearchNoMatchesThenEscRestores verifies Esc after zero-match search restores all rows.
func TestTeatestSearchNoMatchesThenEscRestores(t *testing.T) {
	t.Parallel()
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
