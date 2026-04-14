package app

import (
	"testing"

	"charm.land/bubbles/v2/table"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// makeTab creates a Tab with the given cell data for testing.
// cols is a slice of column names, rows is a slice of rows where each row is a slice of cell values.
// All cells default to cellText kind with Null=false.
func makeTab(cols []string, rows [][]string) *Tab {
	tableCols := make([]table.Column, len(cols))
	specs := make([]columnSpec, len(cols))
	for i, c := range cols {
		tableCols[i] = table.Column{Title: c, Width: 10}
		specs[i] = columnSpec{Title: c, DBName: c, Kind: cellText}
	}

	t := table.New(table.WithColumns(tableCols))

	cellRows := make([][]cell, len(rows))
	tableRows := make([]table.Row, len(rows))
	meta := make([]rowMeta, len(rows))

	for i, row := range rows {
		cells := make([]cell, len(row))
		tRow := make(table.Row, len(row))
		for j, v := range row {
			cells[j] = cell{Value: v, Kind: cellText}
			tRow[j] = v
		}
		cellRows[i] = cells
		tableRows[i] = tRow
		meta[i] = rowMeta{ID: uint(i), RowID: int64(i + 1)}
	}

	t.SetRows(tableRows)

	return &Tab{
		Name:            "test",
		Table:           t,
		Rows:            meta,
		Specs:           specs,
		CellRows:        cellRows,
		Loaded:          true,
		FullRows:        tableRows,
		FullMeta:        meta,
		FullCellRows:    cellRows,
		PostPinRows:     tableRows,
		PostPinMeta:     meta,
		PostPinCellRows: cellRows,
	}
}

// makeTabWithKinds creates a Tab where each column has an explicit cellKind.
func makeTabWithKinds(cols []string, kinds []cellKind, rows [][]string) *Tab {
	tab := makeTab(cols, rows)
	for i := range tab.Specs {
		if i < len(kinds) {
			tab.Specs[i].Kind = kinds[i]
		}
	}
	// Re-stamp cell kinds in CellRows.
	for i, row := range tab.CellRows {
		for j := range row {
			if j < len(kinds) {
				tab.CellRows[i][j].Kind = kinds[j]
				tab.FullCellRows[i][j].Kind = kinds[j]
			}
		}
	}
	return tab
}

// setNullAt marks tab.CellRows[row][col] (and the FullCellRows mirror) as NULL.
func setNullAt(tab *Tab, row, col int) {
	tab.CellRows[row][col].Null = true
	tab.CellRows[row][col].Value = ""
	tab.FullCellRows[row][col].Null = true
	tab.FullCellRows[row][col].Value = ""
}

// ────────────────────────────────────────────────────
// Sort tests
// ────────────────────────────────────────────────────

// 1. toggleSort cycles none → asc → desc → removed
func TestToggleSortCycle(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})

	// Initially no sorts.
	if len(tab.Sorts) != 0 {
		t.Fatalf("expected 0 sorts, got %d", len(tab.Sorts))
	}

	// First toggle: asc.
	tabstate.ToggleSort(tab, 0)
	if len(tab.Sorts) != 1 || tab.Sorts[0].Dir != sortAsc {
		t.Fatalf("expected sortAsc after first toggle, got %+v", tab.Sorts)
	}

	// Second toggle: desc.
	tabstate.ToggleSort(tab, 0)
	if len(tab.Sorts) != 1 || tab.Sorts[0].Dir != sortDesc {
		t.Fatalf("expected sortDesc after second toggle, got %+v", tab.Sorts)
	}

	// Third toggle: removed.
	tabstate.ToggleSort(tab, 0)
	if len(tab.Sorts) != 0 {
		t.Fatalf("expected 0 sorts after third toggle, got %d", len(tab.Sorts))
	}
}

// 2. applySorts with numeric column sorts numerically.
func TestApplySortsNumeric(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "val"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"b", "10"}, {"a", "2"}, {"c", "1"}},
	)
	tab.Sorts = []sortEntry{{Col: 1, Dir: sortAsc}}
	tabstate.ApplySorts(tab)

	want := []string{"1", "2", "10"}
	for i, row := range tab.CellRows {
		if row[1].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[1].Value, want[i])
		}
	}
}

// 3. applySorts with text column sorts case-insensitively.
func TestApplySortsTextCaseInsensitive(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Banana"}, {"apple"}, {"Cherry"}},
	)
	tab.Sorts = []sortEntry{{Col: 0, Dir: sortAsc}}
	tabstate.ApplySorts(tab)

	want := []string{"apple", "Banana", "Cherry"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

// 4. applySorts handles NULL cells (nulls sort last).
func TestApplySortsNullsLast(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "val"},
		[]cellKind{cellText, cellInteger},
		[][]string{{"a", "5"}, {"b", ""}, {"c", "1"}},
	)
	setNullAt(tab, 1, 1) // "b" row, val column is NULL
	tab.Sorts = []sortEntry{{Col: 1, Dir: sortAsc}}
	tabstate.ApplySorts(tab)

	// Null should end up last.
	last := tab.CellRows[len(tab.CellRows)-1]
	if !last[1].Null {
		t.Errorf("expected NULL to sort last, got %+v", last[1])
	}
	// Non-null values should be 1, 5 in ascending order.
	if tab.CellRows[0][1].Value != "1" {
		t.Errorf("row 0 val: got %q, want %q", tab.CellRows[0][1].Value, "1")
	}
	if tab.CellRows[1][1].Value != "5" {
		t.Errorf("row 1 val: got %q, want %q", tab.CellRows[1][1].Value, "5")
	}
}

// 5. clearSorts removes all sorts.
func TestClearSorts(t *testing.T) {
	tab := makeTab([]string{"a", "b"}, [][]string{{"1", "2"}})
	tab.Sorts = []sortEntry{{Col: 0, Dir: sortAsc}, {Col: 1, Dir: sortDesc}}
	tabstate.ClearSorts(tab)
	if len(tab.Sorts) != 0 {
		t.Errorf("expected 0 sorts after clearSorts, got %d", len(tab.Sorts))
	}
}

// 5b. applySorts with no sorts preserves original insertion order.
func TestApplySortsPreservesOriginalOrder(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Charlie"}, {"Alice"}, {"Bob"}},
	)
	// No sorts — should preserve the original order.
	tabstate.ApplySorts(tab)
	want := []string{"Charlie", "Alice", "Bob"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

// 5c. clearSorts + applySorts restores original insertion order.
func TestClearSortsRestoresOriginalOrder(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Charlie"}, {"Alice"}, {"Bob"}},
	)
	// Sort by name ascending.
	tab.Sorts = []sortEntry{{Col: 0, Dir: sortAsc}}
	tabstate.ApplySorts(tab)
	// Verify sorted order.
	sorted := []string{"Alice", "Bob", "Charlie"}
	for i, row := range tab.CellRows {
		if row[0].Value != sorted[i] {
			t.Errorf("sorted row %d: got %q, want %q", i, row[0].Value, sorted[i])
		}
	}
	// Clear sorts and re-apply — should restore original order.
	tabstate.ClearSorts(tab)
	tabstate.ApplySorts(tab)
	want := []string{"Charlie", "Alice", "Bob"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("restored row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

// withPKTiebreaker tests moved to tabstate/tabstate_test.go.
// The behavior is exercised implicitly by ApplySorts tests.

// 7. reorderTab syncs FullRows/FullMeta/FullCellRows with visible arrays.
func TestReorderTab(t *testing.T) {
	tab := makeTab(
		[]string{"id"},
		[][]string{{"A"}, {"B"}, {"C"}},
	)
	// Reorder to [2, 0, 1] → C, A, B.
	tabstate.ReorderTab(tab, []int{2, 0, 1})

	wantCells := []string{"C", "A", "B"}
	for i, row := range tab.CellRows {
		if row[0].Value != wantCells[i] {
			t.Errorf("CellRows[%d]: got %q, want %q", i, row[0].Value, wantCells[i])
		}
	}
	for i, row := range tab.FullCellRows {
		if row[0].Value != wantCells[i] {
			t.Errorf("FullCellRows[%d]: got %q, want %q", i, row[0].Value, wantCells[i])
		}
	}
	tableRows := tab.Table.Rows()
	for i, row := range tableRows {
		if string(row[0]) != wantCells[i] {
			t.Errorf("Table.Rows()[%d]: got %q, want %q", i, row[0], wantCells[i])
		}
	}
	wantIDs := []uint{2, 0, 1}
	for i, m := range tab.FullMeta {
		if m.ID != wantIDs[i] {
			t.Errorf("FullMeta[%d].ID: got %d, want %d", i, m.ID, wantIDs[i])
		}
	}
}

// ────────────────────────────────────────────────────
// Filter tests
// ────────────────────────────────────────────────────

// 8. togglePin adds a new pin; togglePin again removes it.
func TestTogglePin(t *testing.T) {
	tab := makeTab([]string{"name"}, [][]string{{"alice"}})

	pinned := tabstate.TogglePin(tab, 0, "alice")
	if !pinned {
		t.Error("expected togglePin to return true (pinned)")
	}
	if len(tab.Pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(tab.Pins))
	}

	// Toggle off.
	pinned = tabstate.TogglePin(tab, 0, "alice")
	if pinned {
		t.Error("expected togglePin to return false (unpinned)")
	}
	if len(tab.Pins) != 0 {
		t.Errorf("expected 0 pins after removal, got %d", len(tab.Pins))
	}
}

// 9. hasPins returns false on empty, true with pins.
func TestHasPins(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})
	if tabstate.HasPins(tab) {
		t.Error("expected hasPins=false on empty tab")
	}
	tab.Pins = []filterPin{{Col: 0, Values: map[string]bool{"1": true}}}
	if !tabstate.HasPins(tab) {
		t.Error("expected hasPins=true with pins")
	}
}

// 10a. matchesAllPins with single pin.
func TestMatchesAllPinsSingle(t *testing.T) {
	cells := []cell{{Value: "hello", Kind: cellText}}
	pins := []filterPin{{Col: 0, Values: map[string]bool{"hello": true}}}
	if !tabstate.MatchesAllPins(cells, pins) {
		t.Error("expected match for single pin")
	}
}

// 10b. matchesAllPins with multiple pins (AND semantics).
func TestMatchesAllPinsMultiple(t *testing.T) {
	cells := []cell{
		{Value: "alice", Kind: cellText},
		{Value: "30", Kind: cellInteger},
	}
	pins := []filterPin{
		{Col: 0, Values: map[string]bool{"alice": true}},
		{Col: 1, Values: map[string]bool{"30": true}},
	}
	if !tabstate.MatchesAllPins(cells, pins) {
		t.Error("expected match for both pins")
	}

	// Fail if one pin doesn't match.
	pins[1] = filterPin{Col: 1, Values: map[string]bool{"99": true}}
	if tabstate.MatchesAllPins(cells, pins) {
		t.Error("expected no match when second pin fails")
	}
}

// 10c. matchesAllPins with null values.
func TestMatchesAllPinsNull(t *testing.T) {
	cells := []cell{{Value: "", Kind: cellText, Null: true}}
	pins := []filterPin{{Col: 0, Values: map[string]bool{nullPinKey: true}}}
	if !tabstate.MatchesAllPins(cells, pins) {
		t.Error("expected null cell to match nullPinKey")
	}
}

// 11. applyRowFilter with FilterActive=true removes non-matching rows.
func TestApplyRowFilterActive(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	tab.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	tabstate.ApplyRowFilter(tab)

	if len(tab.CellRows) != 2 {
		t.Errorf("expected 2 rows after filter, got %d", len(tab.CellRows))
	}
	for _, row := range tab.CellRows {
		if row[0].Value != "alice" {
			t.Errorf("expected only 'alice' rows, got %q", row[0].Value)
		}
	}
}

// 12. applyRowFilter with FilterInverted=true inverts the match.
func TestApplyRowFilterInverted(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	tab.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	tab.FilterInverted = true
	tabstate.ApplyRowFilter(tab)

	if len(tab.CellRows) != 1 {
		t.Errorf("expected 1 row after inverted filter, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "bob" {
		t.Errorf("expected 'bob', got %q", tab.CellRows[0][0].Value)
	}
}

// 13. clearPins resets all filter state.
func TestClearPins(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})
	tab.Pins = []filterPin{{Col: 0, Values: map[string]bool{"1": true}}}
	tab.FilterActive = true
	tab.FilterInverted = true

	tabstate.ClearPins(tab)

	if len(tab.Pins) != 0 {
		t.Errorf("expected 0 pins, got %d", len(tab.Pins))
	}
	if tab.FilterActive {
		t.Error("expected FilterActive=false after clearPins")
	}
	if tab.FilterInverted {
		t.Error("expected FilterInverted=false after clearPins")
	}
}

// 14. cellDisplayValue for null vs non-null.
func TestCellDisplayValue(t *testing.T) {
	nonNull := cell{Value: "  Hello  ", Kind: cellText}
	if got := tabstate.CellDisplayValue(nonNull); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}

	null := cell{Value: "anything", Kind: cellText, Null: true}
	if got := tabstate.CellDisplayValue(null); got != nullPinKey {
		t.Errorf("expected nullPinKey, got %q", got)
	}
}

// ────────────────────────────────────────────────────
// Type mapping tests
// ────────────────────────────────────────────────────

// 21. sqlTypeToKind maps SQL types to cell kinds.
func TestSQLTypeToKind(t *testing.T) {
	cases := []struct {
		sqlType string
		want    cellKind
	}{
		{"INTEGER", cellInteger},
		{"INT", cellInteger},
		{"BIGINT", cellInteger},
		{"TINYINT", cellInteger},
		{"VARCHAR", cellText},
		{"TEXT", cellText},
		{"DOUBLE", cellReal},
		{"FLOAT", cellReal},
		{"REAL", cellReal},
		{"BOOLEAN", cellText},
		{"BOOL", cellText},
		{"BLOB", cellReadonly},
		{"", cellText},
	}
	for _, tc := range cases {
		got := sqlTypeToKind(tc.sqlType)
		if got != tc.want {
			t.Errorf("sqlTypeToKind(%q) = %d, want %d", tc.sqlType, got, tc.want)
		}
	}
}

// ────────────────────────────────────────────────────
// Sort + filter integration tests
// ────────────────────────────────────────────────────

// 26. Sort then filter: verify filtered rows maintain sort order.
func TestSortThenFilter(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"name", "score"},
		[]cellKind{cellText, cellInteger},
		[][]string{
			{"charlie", "30"},
			{"alice", "10"},
			{"bob", "20"},
			{"alice", "5"},
		},
	)

	// Sort by score ascending.
	tab.Sorts = []sortEntry{{Col: 1, Dir: sortAsc}}
	tabstate.ApplySorts(tab)

	// Now filter to only "alice" rows.
	tab.Pins = []filterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	tabstate.ApplyRowFilter(tab)

	// Should have 2 alice rows, and they should be in ascending score order.
	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows after filter, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][1].Value != "5" || tab.CellRows[1][1].Value != "10" {
		t.Errorf("expected scores [5, 10] in order, got [%s, %s]",
			tab.CellRows[0][1].Value, tab.CellRows[1][1].Value)
	}
}

// ────────────────────────────────────────────────────
// highlightFuzzyPositions tests
// ────────────────────────────────────────────────────

// 28. No positions → entire text in base style.
func TestHighlightFuzzyPositionsNoPositions(t *testing.T) {
	base := ui.TUI.HeaderHint()
	hl := ui.TUI.TextBlueBold()

	result := highlightFuzzyPositions("hello", nil, base, hl)
	want := base.Render("hello")
	if result != want {
		t.Errorf("expected base-styled text, got %q vs %q", result, want)
	}
}

// 29. All positions → entire text in highlight style.
func TestHighlightFuzzyPositionsAllHighlighted(t *testing.T) {
	base := ui.TUI.HeaderHint()
	hl := ui.TUI.TextBlueBold()

	result := highlightFuzzyPositions("abc", []int{0, 1, 2}, base, hl)
	want := hl.Render("abc")
	if result != want {
		t.Errorf("expected all-highlighted text, got %q vs %q", result, want)
	}
}

// 30. Mixed positions → alternating base and highlight runs.
func TestHighlightFuzzyPositionsMixed(t *testing.T) {
	base := ui.TUI.HeaderHint()
	hl := ui.TUI.TextBlueBold()

	// "hello" with positions 0, 3 → "h" highlighted, "el" base, "l" highlighted, "o" base
	result := highlightFuzzyPositions("hello", []int{0, 3}, base, hl)
	want := hl.Render("h") + base.Render("el") + hl.Render("l") + base.Render("o")
	if result != want {
		t.Errorf("expected mixed highlight:\ngot  %q\nwant %q", result, want)
	}
}

// 31. Empty text → empty string.
func TestHighlightFuzzyPositionsEmpty(t *testing.T) {
	base := ui.TUI.HeaderHint()
	hl := ui.TUI.TextBlueBold()

	result := highlightFuzzyPositions("", nil, base, hl)
	want := base.Render("")
	if result != want {
		t.Errorf("expected empty base render, got %q", result)
	}
}

// ────────────────────────────────────────────────────
// gapSeparators tests
// ────────────────────────────────────────────────────

// 32. Contiguous visible columns → all plain separators.
func TestGapSeparatorsContiguous(t *testing.T) {
	visToFull := []int{0, 1, 2, 3}
	normalSep := " | "
	plain, collapsed := gapSeparators(visToFull, 4, normalSep)

	if len(plain) != 3 || len(collapsed) != 3 {
		t.Fatalf("expected 3 separators, got plain=%d, collapsed=%d", len(plain), len(collapsed))
	}
	for i := range plain {
		if plain[i] != normalSep {
			t.Errorf("plain[%d] = %q, want %q", i, plain[i], normalSep)
		}
		if collapsed[i] != normalSep {
			t.Errorf("collapsed[%d] should equal normalSep when no gap, got %q", i, collapsed[i])
		}
	}
}

// 33. Hidden column creates a gap → collapsed separator differs.
func TestGapSeparatorsWithGap(t *testing.T) {
	// Columns 0, 1, 3 visible (column 2 hidden) → gap between indices 1 and 2.
	visToFull := []int{0, 1, 3}
	normalSep := " | "
	plain, collapsed := gapSeparators(visToFull, 4, normalSep)

	if len(plain) != 2 || len(collapsed) != 2 {
		t.Fatalf("expected 2 separators, got plain=%d, collapsed=%d", len(plain), len(collapsed))
	}
	// First gap (0→1): contiguous, should be normalSep.
	if collapsed[0] != normalSep {
		t.Errorf("collapsed[0] should be normalSep for contiguous cols, got %q", collapsed[0])
	}
	// Second gap (1→3): non-contiguous, should differ from normalSep.
	if collapsed[1] == normalSep {
		t.Error("collapsed[1] should differ from normalSep when there's a hidden column gap")
	}
}

// 34. Single column → no separators.
func TestGapSeparatorsSingleColumn(t *testing.T) {
	plain, collapsed := gapSeparators([]int{0}, 1, " | ")
	if plain != nil || collapsed != nil {
		t.Errorf("expected nil separators for single column, got plain=%v, collapsed=%v", plain, collapsed)
	}
}

// ────────────────────────────────────────────────────
// renderHiddenBadges tests
// ────────────────────────────────────────────────────

// 35. No hidden columns → empty string.
func TestRenderHiddenBadgesNone(t *testing.T) {
	specs := []columnSpec{
		{Title: "a", HideOrder: 0},
		{Title: "b", HideOrder: 0},
	}
	result := renderHiddenBadges(specs, 0)
	if result != "" {
		t.Errorf("expected empty string when no columns hidden, got %q", result)
	}
}

// 36. Hidden column to the left of cursor.
func TestRenderHiddenBadgesLeft(t *testing.T) {
	specs := []columnSpec{
		{Title: "a", HideOrder: 1},
		{Title: "b", HideOrder: 0},
		{Title: "c", HideOrder: 0},
	}
	result := renderHiddenBadges(specs, 1) // cursor on "b"
	if result == "" {
		t.Error("expected non-empty badges when column is hidden")
	}
	// Should contain the hidden column name "a".
	if !containsPlainText(result, "a") {
		t.Errorf("expected badge to contain 'a', got %q", result)
	}
}

// 37. Hidden columns on both sides.
func TestRenderHiddenBadgesBothSides(t *testing.T) {
	specs := []columnSpec{
		{Title: "a", HideOrder: 1},
		{Title: "b", HideOrder: 0},
		{Title: "c", HideOrder: 2},
	}
	result := renderHiddenBadges(specs, 1) // cursor on "b"
	if !containsPlainText(result, "a") {
		t.Errorf("expected badge to contain 'a', got %q", result)
	}
	if !containsPlainText(result, "c") {
		t.Errorf("expected badge to contain 'c', got %q", result)
	}
}

// containsPlainText strips ANSI escapes and checks for a substring.
func containsPlainText(s, substr string) bool {
	// Strip ANSI escape sequences for comparison.
	clean := stripANSI(s)
	return len(clean) > 0 && len(substr) > 0 && contains(clean, substr)
}

func stripANSI(s string) string {
	var b []byte
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			// Skip until 'm' (end of ANSI sequence).
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++ // skip the 'm'
			}
			continue
		}
		b = append(b, s[i])
		i++
	}
	return string(b)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ────────────────────────────────────────────────────
// hasSelectableCol tests
// ────────────────────────────────────────────────────

// 38. Tab with selectable columns.
func TestHasSelectableCol(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "name"},
		[]cellKind{cellReadonly, cellText},
		[][]string{{"1", "alice"}},
	)
	if !hasSelectableCol(tab) {
		t.Error("expected hasSelectableCol=true when text column exists")
	}
}

// 39. Tab with only readonly columns.
func TestHasSelectableColAllReadonly(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "rowid"},
		[]cellKind{cellReadonly, cellReadonly},
		[][]string{{"1", "2"}},
	)
	if hasSelectableCol(tab) {
		t.Error("expected hasSelectableCol=false when all columns are readonly")
	}
}

// 40. Tab with all columns hidden.
func TestHasSelectableColAllHidden(t *testing.T) {
	tab := makeTab([]string{"a", "b"}, [][]string{{"1", "2"}})
	tab.Specs[0].HideOrder = 1
	tab.Specs[1].HideOrder = 2
	if hasSelectableCol(tab) {
		t.Error("expected hasSelectableCol=false when all columns are hidden")
	}
}

// ────────────────────────────────────────────────────
// parseSearchQuery tests
// ────────────────────────────────────────────────────

// 41. Plain query with no @ prefix.
func TestParseSearchQueryPlain(t *testing.T) {
	col, terms := match.ParseQuery("hello world")
	if col != "" {
		t.Errorf("expected empty column, got %q", col)
	}
	if terms != "hello world" {
		t.Errorf("expected terms %q, got %q", "hello world", terms)
	}
}

// 42. @column scoping.
func TestParseSearchQueryColumn(t *testing.T) {
	col, terms := match.ParseQuery("@name alice")
	if col != "name" {
		t.Errorf("expected column %q, got %q", "name", col)
	}
	if terms != "alice" {
		t.Errorf("expected terms %q, got %q", "alice", terms)
	}
}

// 43. @column with no search terms.
func TestParseSearchQueryColumnOnly(t *testing.T) {
	col, terms := match.ParseQuery("@status")
	if col != "status" {
		t.Errorf("expected column %q, got %q", "status", col)
	}
	if terms != "" {
		t.Errorf("expected empty terms, got %q", terms)
	}
}

// 44. Empty query.
func TestParseSearchQueryEmpty(t *testing.T) {
	col, terms := match.ParseQuery("")
	if col != "" {
		t.Errorf("expected empty column, got %q", col)
	}
	if terms != "" {
		t.Errorf("expected empty terms, got %q", terms)
	}
}

// 45. @ at start but no column name (just @).
func TestParseSearchQueryBareAt(t *testing.T) {
	col, terms := match.ParseQuery("@ something")
	if col != "" {
		t.Errorf("expected empty column for bare @, got %q", col)
	}
	if terms != "@ something" {
		t.Errorf("expected full string as terms, got %q", terms)
	}
}

// 46. @column with multiple terms.
func TestParseSearchQueryColumnMultipleTerms(t *testing.T) {
	col, terms := match.ParseQuery("@city new york")
	if col != "city" {
		t.Errorf("expected column %q, got %q", "city", col)
	}
	if terms != "new york" {
		t.Errorf("expected terms %q, got %q", "new york", terms)
	}
}

// ────────────────────────────────────────────────────
// applySearchFilter tests
// ────────────────────────────────────────────────────

// 52. Fuzzy search filters rows to matches only.
func TestApplySearchFilterFuzzy(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "paris"}},
	)
	// "alic" uniquely matches "alice" (not "charlie" or "bob").
	state := &rowSearchState{Query: "alic"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "alice" {
		t.Errorf("expected alice, got %q", tab.CellRows[0][0].Value)
	}
}

// 54. Column-scoped search only matches within the specified column.
func TestApplySearchFilterColumnScoped(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"paris", "london"}, {"bob", "paris"}},
	)
	state := &rowSearchState{Query: "paris", Column: "city"}
	applySearchFilter(tab, state, nil)

	// Only row with city=paris should match, not the row with name=paris.
	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][1].Value != "paris" {
		t.Errorf("expected city=paris, got %q", tab.CellRows[0][1].Value)
	}
}

// 55. Empty query is a no-op (all rows shown).
func TestApplySearchFilterEmptyQuery(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}},
	)
	state := &rowSearchState{Query: ""}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 2 {
		t.Errorf("expected 2 rows for empty query, got %d", len(tab.CellRows))
	}
}

// 56. Nil state is a no-op.
func TestApplySearchFilterNilState(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}},
	)
	applySearchFilter(tab, nil, nil)
	if len(tab.CellRows) != 2 {
		t.Errorf("expected 2 rows for nil state, got %d", len(tab.CellRows))
	}
}

// 57. Highlights are populated for matching cells.
func TestApplySearchFilterHighlights(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "london"}, {"bob", "lisbon"}},
	)
	state := &rowSearchState{Query: "li"}
	applySearchFilter(tab, state, nil)

	// Both rows match: "alice" has "li" at pos 1-2, "lisbon" has "li" at pos 0-1.
	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tab.CellRows))
	}
	if state.Highlights == nil {
		t.Fatal("expected non-nil highlights")
	}
	// Row 0 (alice, london): "alice" matches "li" at positions 1,2; "london" has "l" but not "li"
	row0 := state.Highlights[0]
	if row0 == nil {
		t.Fatal("expected highlights for row 0")
	}
	nameHL := row0[0] // "alice" → "li" at positions 1,2
	if len(nameHL) != 2 || nameHL[0] != 1 || nameHL[1] != 2 {
		t.Errorf("expected highlight positions [1,2] for 'alice', got %v", nameHL)
	}
}

// 58. Search composes with pin filter (operates on PostPin data).
func TestApplySearchFilterWithPins(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "paris"}},
	)
	// Pin filter: only paris rows.
	tab.Pins = []filterPin{{Col: 1, Values: map[string]bool{"paris": true}}}
	tab.FilterActive = true
	tabstate.ApplyRowFilter(tab)

	// Now search within the pinned results.
	state := &rowSearchState{Query: "char"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row (charlie in paris), got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "charlie" {
		t.Errorf("expected charlie, got %q", tab.CellRows[0][0].Value)
	}
}

// 59. Invalid column scope matches no rows.
func TestApplySearchFilterInvalidColumn(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}},
	)
	state := &rowSearchState{Query: "alice", Column: "nonexistent"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 0 {
		t.Errorf("expected 0 rows for invalid column, got %d", len(tab.CellRows))
	}
}

// ────────────────────────────────────────────────────
// parseSearchClauses tests
// ────────────────────────────────────────────────────

// 60. Multi-column search with colon syntax.
func TestParseSearchClausesMultiColumn(t *testing.T) {
	groups := match.ParseClauses("@authors: jolly, @year: 2021")
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	clauses := groups[0]
	if len(clauses) != 2 {
		t.Fatalf("expected 2 clauses, got %d", len(clauses))
	}
	if clauses[0].Column != "authors" || clauses[0].Terms != "jolly" {
		t.Errorf("clause 0: got %+v", clauses[0])
	}
	if clauses[1].Column != "year" || clauses[1].Terms != "2021" {
		t.Errorf("clause 1: got %+v", clauses[1])
	}
}

// 61. Single @col: value clause.
func TestParseSearchClausesSingleColon(t *testing.T) {
	groups := match.ParseClauses("@name: alice")
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected 1 group with 1 clause, got %+v", groups)
	}
	if groups[0][0].Column != "name" || groups[0][0].Terms != "alice" {
		t.Errorf("clause: got %+v", groups[0][0])
	}
}

// 62. Plain text (no @) produces a single global clause.
func TestParseSearchClausesPlain(t *testing.T) {
	groups := match.ParseClauses("hello world")
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected 1 group with 1 clause, got %+v", groups)
	}
	if groups[0][0].Column != "" || groups[0][0].Terms != "hello world" {
		t.Errorf("clause: got %+v", groups[0][0])
	}
}

// 63. Legacy @col terms (no colon) still works.
func TestParseSearchClausesLegacy(t *testing.T) {
	groups := match.ParseClauses("@city new york")
	if len(groups) != 1 || len(groups[0]) != 1 {
		t.Fatalf("expected 1 group with 1 clause, got %+v", groups)
	}
	if groups[0][0].Column != "city" || groups[0][0].Terms != "new york" {
		t.Errorf("clause: got %+v", groups[0][0])
	}
}

// 64. Multi-clause AND filter: both clauses must match.
func TestApplySearchFilterMultiClause(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"alice", "london"}},
	)
	state := &rowSearchState{Query: "@name: alice, @city: paris"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "alice" || tab.CellRows[0][1].Value != "paris" {
		t.Errorf("expected alice+paris, got %q+%q", tab.CellRows[0][0].Value, tab.CellRows[0][1].Value)
	}
}

// 65. Multi-clause with one invalid column returns 0 results.
func TestApplySearchFilterMultiClauseInvalidCol(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}},
	)
	state := &rowSearchState{Query: "@name: alice, @bogus: x"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(tab.CellRows))
	}
}

// 66. OR filter: either branch matches.
func TestApplySearchFilterOR(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "tokyo"}},
	)
	state := &rowSearchState{Query: "@city: paris | @city: tokyo"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][1].Value != "paris" {
		t.Errorf("row 0 city: expected paris, got %q", tab.CellRows[0][1].Value)
	}
	if tab.CellRows[1][1].Value != "tokyo" {
		t.Errorf("row 1 city: expected tokyo, got %q", tab.CellRows[1][1].Value)
	}
}

// 67. OR with AND in one branch.
func TestApplySearchFilterORWithAND(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"alice", "london"}, {"bob", "paris"}},
	)
	// Match: (alice AND paris) OR (bob)
	state := &rowSearchState{Query: "@name: alice @city: paris | @name: bob"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "alice" || tab.CellRows[0][1].Value != "paris" {
		t.Errorf("row 0: got %q+%q", tab.CellRows[0][0].Value, tab.CellRows[0][1].Value)
	}
	if tab.CellRows[1][0].Value != "bob" {
		t.Errorf("row 1: expected bob, got %q", tab.CellRows[1][0].Value)
	}
}

// 68. Negate filter: exclude matching rows.
func TestApplySearchFilterNegate(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "paris"}},
	)
	state := &rowSearchState{Query: "@city: -london"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows (excluding london), got %d", len(tab.CellRows))
	}
	for _, row := range tab.CellRows {
		if row[1].Value == "london" {
			t.Error("london row should have been excluded")
		}
	}
}

// 69. Negate combined with positive AND clause.
func TestApplySearchFilterNegateWithAND(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "paris"}},
	)
	// All paris rows, but not charlie.
	state := &rowSearchState{Query: "@city: paris @name: -charlie"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "alice" {
		t.Errorf("expected alice, got %q", tab.CellRows[0][0].Value)
	}
}

// 70. Plain text negate (no column scope).
func TestApplySearchFilterNegatePlain(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"charlie"}},
	)
	state := &rowSearchState{Query: "-bob"}
	applySearchFilter(tab, state, nil)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tab.CellRows))
	}
	for _, row := range tab.CellRows {
		if row[0].Value == "bob" {
			t.Error("bob should have been excluded")
		}
	}
}

// ────────────────────────────────────────────────────
// Fulltext search integration tests
// ────────────────────────────────────────────────────

// mockFTSStore implements data.DataStore + data.FulltextSearcher for testing.
// Only SearchFulltext is functional; all other methods panic if called.
type mockFTSStore struct {
	data.DataStore // embedded to satisfy interface; nil — unused methods panic
	hits           []int64
}

func (m *mockFTSStore) SearchFulltext(_ string, _ []string, _ bool) ([]int64, error) {
	return m.hits, nil
}

// 71. Unscoped query unions fuzzy column matches with FTS hits.
func TestApplySearchFilterFTSUnion(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		// RowIDs: 1=alice, 2=bob, 3=charlie
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "tokyo"}},
	)
	// "xyz" won't fuzzy-match any visible column, but FTS returns rowID 2 (bob).
	store := &mockFTSStore{hits: []int64{2}}
	state := &rowSearchState{Query: "xyz"}
	applySearchFilter(tab, state, store)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row (FTS hit), got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "bob" {
		t.Errorf("expected bob (FTS hit), got %q", tab.CellRows[0][0].Value)
	}
	// FTS-only hits should have no highlights.
	if hl, ok := state.Highlights[0]; ok && len(hl) > 0 {
		t.Errorf("FTS-only hit should have no highlights, got %v", hl)
	}
}

// 72. Column-scoped query does NOT trigger FTS.
func TestApplySearchFilterFTSColumnScoped(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}},
	)
	// FTS would return rowID 2 (bob), but column-scoped query should ignore it.
	store := &mockFTSStore{hits: []int64{2}}
	state := &rowSearchState{Query: "@name: xyz"}
	applySearchFilter(tab, state, store)

	// "xyz" doesn't fuzzy-match any name, and FTS is not used for scoped queries.
	if len(tab.CellRows) != 0 {
		t.Errorf("expected 0 rows for scoped query with no fuzzy match, got %d", len(tab.CellRows))
	}
}

// 73. Negated query does NOT trigger FTS.
func TestApplySearchFilterFTSNegated(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"charlie"}},
	)
	// FTS would return rowID 2, but negated terms should not use FTS.
	store := &mockFTSStore{hits: []int64{2}}
	state := &rowSearchState{Query: "-bob"}
	applySearchFilter(tab, state, store)

	// Negate filters out "bob" via fuzzy → 2 rows.
	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(tab.CellRows))
	}
}

// 74. FTS union: both fuzzy and FTS hits appear.
func TestApplySearchFilterFTSAndFuzzy(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		// RowIDs: 1=alice, 2=bob, 3=charlie
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "tokyo"}},
	)
	// "alice" fuzzy-matches row 1. FTS returns rowID 3 (charlie).
	store := &mockFTSStore{hits: []int64{3}}
	state := &rowSearchState{Query: "alice"}
	applySearchFilter(tab, state, store)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows (fuzzy+FTS), got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "alice" {
		t.Errorf("row 0: expected alice (fuzzy), got %q", tab.CellRows[0][0].Value)
	}
	if tab.CellRows[1][0].Value != "charlie" {
		t.Errorf("row 1: expected charlie (FTS), got %q", tab.CellRows[1][0].Value)
	}
}
