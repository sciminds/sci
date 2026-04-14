package app

import (
	"slices"
	"testing"

	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/uikit"
)

// ────────────────────────────────────────────────────
// Visual mode: effectiveVisualSelection tests
// ────────────────────────────────────────────────────

// Helper to create a Model with a visual state and a tab.
func makeVisualModel(rows [][]string) *Model {
	tab := makeTab([]string{"id", "name"}, rows)
	tab.Table.SetHeight(20)
	m := &Model{
		tabs:   []Tab{*tab},
		active: 0,
		mode:   modeVisual,
		visual: &visualState{
			Anchor:   -1,
			Selected: map[int]bool{},
		},
		styles: uikit.TUI,
	}
	return m
}

// 1. No selection, no anchor → returns cursor row only.
func TestEffectiveVisualSelectionCursorOnly(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}})
	m.tabs[0].Table.SetCursor(1)

	sel := m.effectiveVisualSelection()
	if len(sel) != 1 || sel[0] != 1 {
		t.Errorf("expected [1], got %v", sel)
	}
}

// 2. Space-toggled individual rows.
func TestEffectiveVisualSelectionSpaceToggled(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}, {"4", "d"}})
	m.visual.Selected[0] = true
	m.visual.Selected[3] = true
	m.tabs[0].Table.SetCursor(1) // cursor on 1, but 0 and 3 are selected

	sel := m.effectiveVisualSelection()
	slices.Sort(sel)
	if len(sel) != 2 || sel[0] != 0 || sel[1] != 3 {
		t.Errorf("expected [0 3], got %v", sel)
	}
}

// 3. Anchor range selection (shift+j/k).
func TestEffectiveVisualSelectionAnchorRange(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}, {"4", "d"}, {"5", "e"}})
	m.visual.Anchor = 1
	m.tabs[0].Table.SetCursor(3)

	sel := m.effectiveVisualSelection()
	slices.Sort(sel)
	if len(sel) != 3 || sel[0] != 1 || sel[1] != 2 || sel[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", sel)
	}
}

// 4. Anchor range reversed (cursor above anchor).
func TestEffectiveVisualSelectionAnchorReversed(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}, {"4", "d"}})
	m.visual.Anchor = 3
	m.tabs[0].Table.SetCursor(1)

	sel := m.effectiveVisualSelection()
	slices.Sort(sel)
	if len(sel) != 3 || sel[0] != 1 || sel[1] != 2 || sel[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", sel)
	}
}

// 5. Union of anchor range + space-toggled (no duplicates).
func TestEffectiveVisualSelectionUnion(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}, {"4", "d"}, {"5", "e"}})
	m.visual.Anchor = 1
	m.tabs[0].Table.SetCursor(2) // anchor range: [1,2]
	m.visual.Selected[4] = true  // space-toggled: 4

	sel := m.effectiveVisualSelection()
	slices.Sort(sel)
	if len(sel) != 3 || sel[0] != 1 || sel[1] != 2 || sel[2] != 4 {
		t.Errorf("expected [1 2 4], got %v", sel)
	}
}

// 6. Union deduplicates overlapping indices.
func TestEffectiveVisualSelectionDedup(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}})
	m.visual.Anchor = 0
	m.tabs[0].Table.SetCursor(2)
	m.visual.Selected[1] = true // already in anchor range

	sel := m.effectiveVisualSelection()
	slices.Sort(sel)
	if len(sel) != 3 || sel[0] != 0 || sel[1] != 1 || sel[2] != 2 {
		t.Errorf("expected [0 1 2], got %v", sel)
	}
}

// ────────────────────────────────────────────────────
// Visual mode: enter/exit
// ────────────────────────────────────────────────────

// 7. Enter visual mode on writable tab.
func TestEnterVisualMode(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}})
	m.mode = modeNormal
	m.visual = nil

	m.enterVisualMode()

	if m.mode != modeVisual {
		t.Errorf("expected modeVisual, got %d", m.mode)
	}
	if m.visual == nil {
		t.Fatal("expected visual state to be initialized")
	}
	if m.visual.Anchor != -1 {
		t.Errorf("expected anchor -1, got %d", m.visual.Anchor)
	}
	if len(m.visual.Selected) != 0 {
		t.Errorf("expected empty Selected, got %v", m.visual.Selected)
	}
}

// 8. Exit visual mode clears state.
func TestExitVisualMode(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}})
	m.visual.Selected[0] = true
	m.visual.Anchor = 0

	m.exitVisualMode()

	if m.mode != modeNormal {
		t.Errorf("expected modeNormal, got %d", m.mode)
	}
	if m.visual != nil {
		t.Error("expected visual state to be nil after exit")
	}
}

// 9. Visual mode blocked on read-only tab.
func TestVisualModeBlockedReadOnly(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}})
	m.mode = modeNormal
	m.visual = nil
	m.tabs[0].ReadOnly = true

	// Attempt to enter visual mode.
	m.enterVisualMode()

	// Should stay in normal mode.
	if m.mode != modeNormal {
		t.Errorf("expected modeNormal for read-only tab, got %d", m.mode)
	}
	if m.visual != nil {
		t.Error("visual state should be nil for read-only tab")
	}
}

// ────────────────────────────────────────────────────
// Visual mode: yank to internal clipboard
// ────────────────────────────────────────────────────

// 10. Yank copies selected row data into clipboard.
func TestVisualYank(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}})
	m.visual.Selected[0] = true
	m.visual.Selected[2] = true

	m.visualYank()

	if len(m.clipboard) != 2 {
		t.Fatalf("expected 2 clipboard rows, got %d", len(m.clipboard))
	}
	if m.clipboard[0][0].Value != "1" || m.clipboard[0][1].Value != "a" {
		t.Errorf("clipboard row 0: got %v", m.clipboard[0])
	}
	if m.clipboard[1][0].Value != "3" || m.clipboard[1][1].Value != "c" {
		t.Errorf("clipboard row 1: got %v", m.clipboard[1])
	}
}

// 11. Yank with no explicit selection copies cursor row.
func TestVisualYankCursorRow(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}})
	m.tabs[0].Table.SetCursor(1)

	m.visualYank()

	if len(m.clipboard) != 1 {
		t.Fatalf("expected 1 clipboard row, got %d", len(m.clipboard))
	}
	if m.clipboard[0][0].Value != "2" {
		t.Errorf("expected '2', got %q", m.clipboard[0][0].Value)
	}
}

// ────────────────────────────────────────────────────
// Visual mode: TSV formatting for system clipboard
// ────────────────────────────────────────────────────

// 12. formatRowsTSV produces correct TSV output.
func TestFormatRowsTSV(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "hello"}, {"2", "world"}})
	m.visual.Selected[0] = true
	m.visual.Selected[1] = true

	tab := m.effectiveTab()
	sel := m.effectiveVisualSelection()
	tsv := formatRowsTSV(tab, sel)

	want := "id\tname\n1\thello\n2\tworld\n"
	if tsv != want {
		t.Errorf("expected %q, got %q", want, tsv)
	}
}

// ────────────────────────────────────────────────────
// Visual mode: isVisualSelected helper
// ────────────────────────────────────────────────────

// ────────────────────────────────────────────────────
// Visual mode: restoreTabState tests
// ────────────────────────────────────────────────────

// 14. restoreTabState preserves sorts.
func TestRestoreTabStateSorts(t *testing.T) {
	old := makeTabWithKinds(
		[]string{"id", "val"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"b", "2"}, {"a", "1"}, {"c", "3"}},
	)
	old.Sorts = []sortEntry{{Col: 1, Dir: sortAsc}}
	tabstate.ApplySorts(old)
	old.Table.SetHeight(20)

	// Simulate a rebuild: new tab has unsorted data.
	newTab := makeTabWithKinds(
		[]string{"id", "val"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"b", "2"}, {"a", "1"}, {"c", "3"}},
	)
	restoreTabState(newTab, old, 0)

	// Should be sorted by val ascending: 1, 2, 3.
	want := []string{"1", "2", "3"}
	for i, row := range newTab.CellRows {
		if row[1].Value != want[i] {
			t.Errorf("row %d val: got %q, want %q", i, row[1].Value, want[i])
		}
	}
}

// 15. restoreTabState preserves active filter.
func TestRestoreTabStateFilter(t *testing.T) {
	old := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	old.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	old.FilterActive = true
	old.Table.SetHeight(20)

	newTab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	restoreTabState(newTab, old, 0)

	// Should show only alice rows.
	if len(newTab.CellRows) != 2 {
		t.Fatalf("expected 2 filtered rows, got %d", len(newTab.CellRows))
	}
	for _, row := range newTab.CellRows {
		if row[0].Value != "alice" {
			t.Errorf("expected only 'alice', got %q", row[0].Value)
		}
	}
	if !newTab.FilterActive {
		t.Error("expected FilterActive to be preserved")
	}
}

// 16. restoreTabState preserves inverted filter.
func TestRestoreTabStateFilterInverted(t *testing.T) {
	old := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	old.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	old.FilterActive = true
	old.FilterInverted = true
	old.Table.SetHeight(20)

	newTab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	restoreTabState(newTab, old, 0)

	// Inverted: should show only bob.
	if len(newTab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(newTab.CellRows))
	}
	if newTab.CellRows[0][0].Value != "bob" {
		t.Errorf("expected 'bob', got %q", newTab.CellRows[0][0].Value)
	}
	if !newTab.FilterInverted {
		t.Error("expected FilterInverted to be preserved")
	}
}

// 17. restoreTabState clamps cursor when rows are deleted.
func TestRestoreTabStateCursorClamp(t *testing.T) {
	old := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}})
	old.Table.SetHeight(20)

	// New tab has fewer rows (simulating deletion).
	newTab := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}})
	restoreTabState(newTab, old, 4) // old cursor was at row 4, but new only has 2 rows

	cursor := newTab.Table.Cursor()
	if cursor != 1 {
		t.Errorf("expected cursor clamped to 1 (last row), got %d", cursor)
	}
}

// 18. restoreTabState preserves cursor when in range.
func TestRestoreTabStateCursorPreserved(t *testing.T) {
	old := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}, {"3"}})
	old.Table.SetHeight(20)

	newTab := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}, {"3"}})
	restoreTabState(newTab, old, 2)

	cursor := newTab.Table.Cursor()
	if cursor != 2 {
		t.Errorf("expected cursor at 2, got %d", cursor)
	}
}

// 19. restoreTabState preserves column cursor and view offset.
func TestRestoreTabStateColCursor(t *testing.T) {
	old := makeTab([]string{"a", "b", "c"}, [][]string{{"1", "2", "3"}})
	old.ColCursor = 2
	old.ViewOffset = 1
	old.Table.SetHeight(20)

	newTab := makeTab([]string{"a", "b", "c"}, [][]string{{"1", "2", "3"}})
	restoreTabState(newTab, old, 0)

	if newTab.ColCursor != 2 {
		t.Errorf("expected ColCursor=2, got %d", newTab.ColCursor)
	}
	if newTab.ViewOffset != 1 {
		t.Errorf("expected ViewOffset=1, got %d", newTab.ViewOffset)
	}
}

// 20. restoreTabState with sort + filter combined.
func TestRestoreTabStateSortAndFilter(t *testing.T) {
	old := makeTabWithKinds(
		[]string{"name", "score"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"alice", "30"}, {"bob", "10"}, {"alice", "5"}},
	)
	old.Sorts = []sortEntry{{Col: 1, Dir: sortAsc}}
	old.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	old.FilterActive = true
	old.Table.SetHeight(20)
	tabstate.ApplySorts(old)
	tabstate.ApplyRowFilter(old)

	newTab := makeTabWithKinds(
		[]string{"name", "score"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"alice", "30"}, {"bob", "10"}, {"alice", "5"}},
	)
	restoreTabState(newTab, old, 0)

	// Should have 2 alice rows, sorted by score ascending (5, 30).
	if len(newTab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(newTab.CellRows))
	}
	if newTab.CellRows[0][1].Value != "5" || newTab.CellRows[1][1].Value != "30" {
		t.Errorf("expected scores [5, 30], got [%s, %s]",
			newTab.CellRows[0][1].Value, newTab.CellRows[1][1].Value)
	}
}

// 21. restoreTabState with no pins does not apply filter.
func TestRestoreTabStateNoPins(t *testing.T) {
	old := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}, {"3"}})
	old.Table.SetHeight(20)

	newTab := makeTab([]string{"id"}, [][]string{{"1"}, {"2"}, {"3"}})
	restoreTabState(newTab, old, 1)

	// All rows should be visible.
	if len(newTab.CellRows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(newTab.CellRows))
	}
}

// 13. isVisualSelected returns correct map.
func TestIsVisualSelected(t *testing.T) {
	m := makeVisualModel([][]string{{"1", "a"}, {"2", "b"}, {"3", "c"}})
	m.visual.Selected[0] = true
	m.visual.Selected[2] = true

	sel := m.visualSelectionSet()

	if !sel[0] || sel[1] || !sel[2] {
		t.Errorf("expected {0:true, 2:true}, got %v", sel)
	}
}
