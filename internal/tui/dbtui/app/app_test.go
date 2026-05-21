package app

import (
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/table"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/uikit"
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

// ────────────────────────────────────────────────────
// Sort + filter + reorder unit tests live in
// tabstate/tabstate_test.go. The app layer just calls
// tabstate.ToggleSort/ApplySorts/ClearSorts/ReorderTab/TogglePin/HasPins/
// MatchesAllPins/ApplyRowFilter/ClearPins/CellDisplayValue, all of which
// are covered there.
// ────────────────────────────────────────────────────

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
// highlightFuzzyPositions tests
// ────────────────────────────────────────────────────

// 28. No positions → entire text in base style.
func TestHighlightFuzzyPositionsNoPositions(t *testing.T) {
	base := uikit.TUI.HeaderHint()
	hl := uikit.TUI.TextBlueBold()

	result := highlightFuzzyPositions("hello", nil, base, hl)
	want := base.Render("hello")
	if result != want {
		t.Errorf("expected base-styled text, got %q vs %q", result, want)
	}
}

// 29. All positions → entire text in highlight style.
func TestHighlightFuzzyPositionsAllHighlighted(t *testing.T) {
	base := uikit.TUI.HeaderHint()
	hl := uikit.TUI.TextBlueBold()

	result := highlightFuzzyPositions("abc", []int{0, 1, 2}, base, hl)
	want := hl.Render("abc")
	if result != want {
		t.Errorf("expected all-highlighted text, got %q vs %q", result, want)
	}
}

// 30. Mixed positions → alternating base and highlight runs.
func TestHighlightFuzzyPositionsMixed(t *testing.T) {
	base := uikit.TUI.HeaderHint()
	hl := uikit.TUI.TextBlueBold()

	// "hello" with positions 0, 3 → "h" highlighted, "el" base, "l" highlighted, "o" base
	result := highlightFuzzyPositions("hello", []int{0, 3}, base, hl)
	want := hl.Render("h") + base.Render("el") + hl.Render("l") + base.Render("o")
	if result != want {
		t.Errorf("expected mixed highlight:\ngot  %q\nwant %q", result, want)
	}
}

// 31. Empty text → empty string.
func TestHighlightFuzzyPositionsEmpty(t *testing.T) {
	base := uikit.TUI.HeaderHint()
	hl := uikit.TUI.TextBlueBold()

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
// match.ParseQuery and match.ParseClauses unit tests live in
// match/match_test.go. The applySearchFilter tests below
// exercise the app-level wrapper.
// ────────────────────────────────────────────────────

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, nil, modeDefault, nil, nil)
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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

	if len(tab.CellRows) != 0 {
		t.Errorf("expected 0 rows for invalid column, got %d", len(tab.CellRows))
	}
}

// 64. Multi-clause AND filter: both clauses must match.
func TestApplySearchFilterMultiClause(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"alice", "london"}},
	)
	state := &rowSearchState{Query: "@name: alice, @city: paris"}
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
	applySearchFilter(tab, state, modeDefault, nil, nil)

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
// Every call is recorded in calls for assertion on exact/prefix semantics.
type mockFTSStore struct {
	store.DataStore // embedded to satisfy interface; nil — unused methods panic
	hits            []int64
	calls           []mockFTSCall
}

type mockFTSCall struct {
	words []string
	exact bool
}

func (m *mockFTSStore) SearchFulltext(_ string, words []string, exact bool) ([]int64, error) {
	m.calls = append(m.calls, mockFTSCall{words: slices.Clone(words), exact: exact})
	return m.hits, nil
}

// makeFTSModel builds a Model with an FTS-capable store and a single loaded tab.
// The search bar is opened and query pre-filled so tests can exercise the async
// FTS flow (handleFTSTick → ftsResultMsg).
func makeFTSModel(hits []int64, query string) *Model {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}, {"bob", "london"}, {"charlie", "tokyo"}},
	)
	m := minimalModel()
	m.store = &mockFTSStore{hits: hits}
	m.tabs = []Tab{*tab}
	m.active = 0
	m.width = 80
	m.height = 24
	// FTS-oriented tests always want full-mode semantics (FTS + note-body
	// hits contribute to the row set). Default mode would ignore them.
	m.searchMode = modeFull
	// Open search and set query.
	m.openSearch()
	m.search.Query = query
	tabstate.SnapshotPostPin(&m.tabs[0])
	return m
}

// 75. handleFTSTick returns a tea.Cmd (non-blocking) and sets ftsLoading.
func TestHandleFTSTickReturnsCmd(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 1

	cmd := m.handleFTSTick(ftsTickMsg{Seq: 1})
	if cmd == nil {
		t.Fatal("handleFTSTick should return a non-nil tea.Cmd for async FTS query")
	}
	if !m.search.ftsLoading {
		t.Error("ftsLoading should be true while FTS query is in flight")
	}
}

// 76. handleFTSTick ignores stale ticks (seq mismatch).
func TestHandleFTSTickStaleIgnored(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 5

	cmd := m.handleFTSTick(ftsTickMsg{Seq: 3}) // stale
	if cmd != nil {
		t.Error("stale ftsTickMsg should return nil cmd")
	}
	if m.search.ftsLoading {
		t.Error("ftsLoading should remain false for stale tick")
	}
}

// 77. ftsResultMsg updates cached hits, clears ftsLoading, and re-filters.
func TestFTSResultMsgUpdatesHits(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 1
	m.search.ftsLoading = true

	// Simulate the result message arriving.
	msg := ftsResultMsg{Seq: 1, Hits: map[int64]bool{2: true}}
	m.handleFTSResult(msg)

	if m.search.ftsLoading {
		t.Error("ftsLoading should be false after result arrives")
	}
	if m.search.ftsHits == nil || !m.search.ftsHits[2] {
		t.Error("ftsHits should contain rowID 2")
	}
	// "xyz" doesn't fuzzy-match any column, but FTS hit rowID 2 (bob) should survive.
	tab := &m.tabs[m.active]
	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row after FTS result, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "bob" {
		t.Errorf("expected bob (FTS hit), got %q", tab.CellRows[0][0].Value)
	}
}

// 78. Stale ftsResultMsg is discarded (seq mismatch).
func TestFTSResultMsgStaleDiscarded(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 5
	m.search.ftsLoading = true

	msg := ftsResultMsg{Seq: 3, Hits: map[int64]bool{2: true}} // stale
	m.handleFTSResult(msg)

	// ftsLoading stays true because the stale result was ignored —
	// only a fresh result (seq=5) should clear it.
	if !m.search.ftsLoading {
		t.Error("ftsLoading should remain true when stale result is discarded")
	}
	if m.search.ftsHits != nil {
		t.Error("ftsHits should not be updated by stale result")
	}
}

// 79. ftsResultMsg is wired in Update and returns spinner tick.
func TestUpdateRoutesFTSResultMsg(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 1
	m.search.ftsLoading = true

	_, cmd := m.Update(ftsResultMsg{Seq: 1, Hits: map[int64]bool{2: true}})
	// Should return a cmd (spinner tick continues).
	_ = cmd // not nil-checking the spinner tick — just verifying no panic
	if m.search.ftsLoading {
		t.Error("ftsLoading should be false after Update processes ftsResultMsg")
	}
}

// 80. Update routes ftsTickMsg and returns a non-nil cmd (the async query).
func TestUpdateRoutesFTSTickMsg(t *testing.T) {
	m := makeFTSModel([]int64{2}, "xyz")
	m.search.ftsSeq = 1

	_, cmd := m.Update(ftsTickMsg{Seq: 1})
	if cmd == nil {
		t.Fatal("Update(ftsTickMsg) should return a non-nil cmd for the async FTS query")
	}
}

// 81. renderSearchBar shows spinner indicator when ftsLoading is true.
func TestSearchBarShowsLoadingIndicator(t *testing.T) {
	m := makeFTSModel([]int64{}, "xyz")
	m.search.ftsLoading = true

	bar := m.renderSearchBar()
	// The bar should contain a loading indicator (not the numeric count).
	if !strings.Contains(bar, "searching") {
		t.Errorf("search bar should show 'searching' during FTS load, got: %s", bar)
	}
}

// 82. View does not panic when ftsLoading is true.
func TestViewWithFTSLoading(t *testing.T) {
	m := makeFTSModel([]int64{}, "test")
	m.search.ftsLoading = true
	_ = m.View() // must not panic
}

// BuildFTSHitSet: single unquoted token uses prefix match — supports
// incremental typing ("neuro" → "neuroimaging").
func TestBuildFTSHitSet_SingleTokenUsesPrefix(t *testing.T) {
	store := &mockFTSStore{hits: []int64{2}}
	groups := match.ParseClauses("neuro")
	_ = buildFTSHitSet(groups, store, "items")

	if len(store.calls) == 0 {
		t.Fatal("expected SearchFulltext call")
	}
	prefixCall, ok := lo.Find(store.calls, func(c mockFTSCall) bool { return !c.exact })
	if !ok {
		t.Errorf("expected a prefix (exact=false) call for single token, got %+v", store.calls)
	}
	if len(prefixCall.words) != 1 || prefixCall.words[0] != "neuro" {
		t.Errorf("expected prefix call with [neuro], got %v", prefixCall.words)
	}
}

// BuildFTSHitSet: multi-token queries upgrade unquoted tokens to exact-word
// match. Prefix expansion on every token across a large library produces
// far too many false positives (e.g. "drives" prefix hits
// "drove/driver/driven"). Once the user has typed multiple words the
// intent is no longer incremental — strict word match is what they want.
func TestBuildFTSHitSet_MultiTokenUsesExact(t *testing.T) {
	store := &mockFTSStore{hits: []int64{2}}
	groups := match.ParseClauses("gossip drives")
	_ = buildFTSHitSet(groups, store, "items")

	// No prefix call should be made; every call must be exact=true.
	for _, c := range store.calls {
		if !c.exact {
			t.Errorf("expected all calls exact=true for multi-token query, got %+v", c)
		}
	}
	// Both words must have been searched exactly.
	exactWords := lo.FlatMap(store.calls, func(c mockFTSCall, _ int) []string {
		if !c.exact {
			return nil
		}
		return c.words
	})
	if !lo.Contains(exactWords, "gossip") || !lo.Contains(exactWords, "drives") {
		t.Errorf("expected both 'gossip' and 'drives' searched exactly, got %v", exactWords)
	}
}

// BuildFTSHitSet: explicitly quoted tokens stay exact regardless of count.
func TestBuildFTSHitSet_QuotedStaysExact(t *testing.T) {
	store := &mockFTSStore{hits: []int64{2}}
	groups := match.ParseClauses(`"brain"`)
	_ = buildFTSHitSet(groups, store, "items")

	for _, c := range store.calls {
		if !c.exact {
			t.Errorf("quoted token must be exact, got %+v", c)
		}
	}
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
	groups := match.ParseClauses(state.Query)
	ftsHits := buildFTSHitSet(groups, store, tab.Name)
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

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
	groups := match.ParseClauses(state.Query)
	ftsHits := buildFTSHitSet(groups, store, tab.Name)
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

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
	groups := match.ParseClauses(state.Query)
	ftsHits := buildFTSHitSet(groups, store, tab.Name)
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

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
	groups := match.ParseClauses(state.Query)
	ftsHits := buildFTSHitSet(groups, store, tab.Name)
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

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
