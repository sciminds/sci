package tabstate

import (
	"testing"

	"charm.land/bubbles/v2/table"
)

// ── Test helpers ─────────────────────────────────────────────────────────────

// makeTab creates a Tab with the given cell data for testing.
// All cells default to CellText kind with Null=false.
func makeTab(cols []string, rows [][]string) *Tab {
	tableCols := make([]table.Column, len(cols))
	specs := make([]ColumnSpec, len(cols))
	for i, c := range cols {
		tableCols[i] = table.Column{Title: c, Width: 10}
		specs[i] = ColumnSpec{Title: c, DBName: c, Kind: CellText}
	}

	t := table.New(table.WithColumns(tableCols))

	cellRows := make([][]Cell, len(rows))
	tableRows := make([]table.Row, len(rows))
	meta := make([]RowMeta, len(rows))

	for i, row := range rows {
		cells := make([]Cell, len(row))
		tRow := make(table.Row, len(row))
		for j, v := range row {
			cells[j] = Cell{Value: v, Kind: CellText}
			tRow[j] = v
		}
		cellRows[i] = cells
		tableRows[i] = tRow
		meta[i] = RowMeta{ID: uint(i), RowID: int64(i + 1)}
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

// makeTabWithKinds creates a Tab where each column has an explicit CellKind.
func makeTabWithKinds(cols []string, kinds []CellKind, rows [][]string) *Tab {
	tab := makeTab(cols, rows)
	for i := range tab.Specs {
		if i < len(kinds) {
			tab.Specs[i].Kind = kinds[i]
		}
	}
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

// ── Sort tests ───────────────────────────────────────────────────────────────

func TestToggleSortCycle(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})

	if len(tab.Sorts) != 0 {
		t.Fatalf("expected 0 sorts, got %d", len(tab.Sorts))
	}

	ToggleSort(tab, 0)
	if len(tab.Sorts) != 1 || tab.Sorts[0].Dir != SortAsc {
		t.Fatalf("expected SortAsc after first toggle, got %+v", tab.Sorts)
	}

	ToggleSort(tab, 0)
	if len(tab.Sorts) != 1 || tab.Sorts[0].Dir != SortDesc {
		t.Fatalf("expected SortDesc after second toggle, got %+v", tab.Sorts)
	}

	ToggleSort(tab, 0)
	if len(tab.Sorts) != 0 {
		t.Fatalf("expected 0 sorts after third toggle, got %d", len(tab.Sorts))
	}
}

func TestApplySortsNumeric(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "val"},
		[]CellKind{CellText, CellInteger},
		[][]string{{"b", "10"}, {"a", "2"}, {"c", "1"}},
	)
	tab.Sorts = []SortEntry{{Col: 1, Dir: SortAsc}}
	ApplySorts(tab)

	want := []string{"1", "2", "10"}
	for i, row := range tab.CellRows {
		if row[1].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[1].Value, want[i])
		}
	}
}

func TestApplySortsTextCaseInsensitive(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Banana"}, {"apple"}, {"Cherry"}},
	)
	tab.Sorts = []SortEntry{{Col: 0, Dir: SortAsc}}
	ApplySorts(tab)

	want := []string{"apple", "Banana", "Cherry"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

func TestApplySortsNullsLast(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"id", "val"},
		[]CellKind{CellText, CellInteger},
		[][]string{{"a", "5"}, {"b", ""}, {"c", "1"}},
	)
	setNullAt(tab, 1, 1)
	tab.Sorts = []SortEntry{{Col: 1, Dir: SortAsc}}
	ApplySorts(tab)

	last := tab.CellRows[len(tab.CellRows)-1]
	if !last[1].Null {
		t.Errorf("expected NULL to sort last, got %+v", last[1])
	}
	if tab.CellRows[0][1].Value != "1" {
		t.Errorf("row 0 val: got %q, want %q", tab.CellRows[0][1].Value, "1")
	}
	if tab.CellRows[1][1].Value != "5" {
		t.Errorf("row 1 val: got %q, want %q", tab.CellRows[1][1].Value, "5")
	}
}

func TestClearSorts(t *testing.T) {
	tab := makeTab([]string{"a", "b"}, [][]string{{"1", "2"}})
	tab.Sorts = []SortEntry{{Col: 0, Dir: SortAsc}, {Col: 1, Dir: SortDesc}}
	ClearSorts(tab)
	if len(tab.Sorts) != 0 {
		t.Errorf("expected 0 sorts after ClearSorts, got %d", len(tab.Sorts))
	}
}

func TestApplySortsPreservesOriginalOrder(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Charlie"}, {"Alice"}, {"Bob"}},
	)
	ApplySorts(tab)
	want := []string{"Charlie", "Alice", "Bob"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

func TestClearSortsRestoresOriginalOrder(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"Charlie"}, {"Alice"}, {"Bob"}},
	)
	tab.Sorts = []SortEntry{{Col: 0, Dir: SortAsc}}
	ApplySorts(tab)
	sorted := []string{"Alice", "Bob", "Charlie"}
	for i, row := range tab.CellRows {
		if row[0].Value != sorted[i] {
			t.Errorf("sorted row %d: got %q, want %q", i, row[0].Value, sorted[i])
		}
	}
	ClearSorts(tab)
	ApplySorts(tab)
	want := []string{"Charlie", "Alice", "Bob"}
	for i, row := range tab.CellRows {
		if row[0].Value != want[i] {
			t.Errorf("restored row %d: got %q, want %q", i, row[0].Value, want[i])
		}
	}
}

func TestReorderTab(t *testing.T) {
	tab := makeTab(
		[]string{"id"},
		[][]string{{"A"}, {"B"}, {"C"}},
	)
	ReorderTab(tab, []int{2, 0, 1})

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

// ── Filter tests ─────────────────────────────────────────────────────────────

func TestTogglePin(t *testing.T) {
	tab := makeTab([]string{"name"}, [][]string{{"alice"}})

	pinned := TogglePin(tab, 0, "alice")
	if !pinned {
		t.Error("expected TogglePin to return true (pinned)")
	}
	if len(tab.Pins) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(tab.Pins))
	}

	pinned = TogglePin(tab, 0, "alice")
	if pinned {
		t.Error("expected TogglePin to return false (unpinned)")
	}
	if len(tab.Pins) != 0 {
		t.Errorf("expected 0 pins after removal, got %d", len(tab.Pins))
	}
}

func TestHasPins(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})
	if HasPins(tab) {
		t.Error("expected HasPins=false on empty tab")
	}
	tab.Pins = []FilterPin{{Col: 0, Values: map[string]bool{"1": true}}}
	if !HasPins(tab) {
		t.Error("expected HasPins=true with pins")
	}
}

func TestMatchesAllPinsSingle(t *testing.T) {
	cells := []Cell{{Value: "hello", Kind: CellText}}
	pins := []FilterPin{{Col: 0, Values: map[string]bool{"hello": true}}}
	if !MatchesAllPins(cells, pins) {
		t.Error("expected match for single pin")
	}
}

func TestMatchesAllPinsMultiple(t *testing.T) {
	cells := []Cell{
		{Value: "alice", Kind: CellText},
		{Value: "30", Kind: CellInteger},
	}
	pins := []FilterPin{
		{Col: 0, Values: map[string]bool{"alice": true}},
		{Col: 1, Values: map[string]bool{"30": true}},
	}
	if !MatchesAllPins(cells, pins) {
		t.Error("expected match for both pins")
	}

	pins[1] = FilterPin{Col: 1, Values: map[string]bool{"99": true}}
	if MatchesAllPins(cells, pins) {
		t.Error("expected no match when second pin fails")
	}
}

func TestMatchesAllPinsNull(t *testing.T) {
	cells := []Cell{{Value: "", Kind: CellText, Null: true}}
	pins := []FilterPin{{Col: 0, Values: map[string]bool{NullPinKey: true}}}
	if !MatchesAllPins(cells, pins) {
		t.Error("expected null cell to match NullPinKey")
	}
}

func TestApplyRowFilterActive(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	tab.Pins = []FilterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	ApplyRowFilter(tab)

	if len(tab.CellRows) != 2 {
		t.Errorf("expected 2 rows after filter, got %d", len(tab.CellRows))
	}
	for _, row := range tab.CellRows {
		if row[0].Value != "alice" {
			t.Errorf("expected only 'alice' rows, got %q", row[0].Value)
		}
	}
}

func TestApplyRowFilterInverted(t *testing.T) {
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}, {"alice"}},
	)
	tab.Pins = []FilterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	tab.FilterInverted = true
	ApplyRowFilter(tab)

	if len(tab.CellRows) != 1 {
		t.Errorf("expected 1 row after inverted filter, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][0].Value != "bob" {
		t.Errorf("expected 'bob', got %q", tab.CellRows[0][0].Value)
	}
}

func TestClearPins(t *testing.T) {
	tab := makeTab([]string{"a"}, [][]string{{"1"}})
	tab.Pins = []FilterPin{{Col: 0, Values: map[string]bool{"1": true}}}
	tab.FilterActive = true
	tab.FilterInverted = true

	ClearPins(tab)

	if len(tab.Pins) != 0 {
		t.Errorf("expected 0 pins, got %d", len(tab.Pins))
	}
	if tab.FilterActive {
		t.Error("expected FilterActive=false after ClearPins")
	}
	if tab.FilterInverted {
		t.Error("expected FilterInverted=false after ClearPins")
	}
}

func TestCellDisplayValue(t *testing.T) {
	nonNull := Cell{Value: "  Hello  ", Kind: CellText}
	if got := CellDisplayValue(nonNull); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}

	null := Cell{Value: "anything", Kind: CellText, Null: true}
	if got := CellDisplayValue(null); got != NullPinKey {
		t.Errorf("expected NullPinKey, got %q", got)
	}
}

// ── Sort + filter integration ────────────────────────────────────────────────

func TestSortThenFilter(t *testing.T) {
	tab := makeTabWithKinds(
		[]string{"name", "score"},
		[]CellKind{CellText, CellInteger},
		[][]string{
			{"charlie", "30"},
			{"alice", "10"},
			{"bob", "20"},
			{"alice", "5"},
		},
	)

	tab.Sorts = []SortEntry{{Col: 1, Dir: SortAsc}}
	ApplySorts(tab)

	tab.Pins = []FilterPin{{Col: 0, Values: map[string]bool{"alice": true}}}
	tab.FilterActive = true
	ApplyRowFilter(tab)

	if len(tab.CellRows) != 2 {
		t.Fatalf("expected 2 rows after filter, got %d", len(tab.CellRows))
	}
	if tab.CellRows[0][1].Value != "5" || tab.CellRows[1][1].Value != "10" {
		t.Errorf("expected scores [5, 10] in order, got [%s, %s]",
			tab.CellRows[0][1].Value, tab.CellRows[1][1].Value)
	}
}
