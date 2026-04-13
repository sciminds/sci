package tabstate

// sort.go — multi-column sort: toggle asc/desc/none per column, apply sort
// order to table rows, and render sort indicators in column headers.

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/samber/lo"
)

// ToggleSort cycles a column through the sort states: none → asc → desc → none.
func ToggleSort(tab *Tab, colIdx int) {
	for i, entry := range tab.Sorts {
		if entry.Col == colIdx {
			if entry.Dir == SortAsc {
				tab.Sorts[i].Dir = SortDesc
			} else {
				tab.Sorts = append(tab.Sorts[:i], tab.Sorts[i+1:]...)
			}
			return
		}
	}
	tab.Sorts = append(tab.Sorts, SortEntry{Col: colIdx, Dir: SortAsc})
}

// ClearSorts removes all sort entries from the tab.
func ClearSorts(tab *Tab) {
	tab.Sorts = nil
}

// ApplySorts reorders all row arrays (CellRows, Rows, Table.Rows, Full*)
// according to the current sort entries. With no sorts, rows are restored
// to their original insertion order (by RowMeta.ID).
func ApplySorts(tab *Tab) {
	if len(tab.CellRows) <= 1 {
		return
	}

	// No user sorts — restore original insertion order via RowMeta.ID.
	if len(tab.Sorts) == 0 {
		indices := lo.Times(len(tab.CellRows), func(i int) int { return i })
		slices.SortStableFunc(indices, func(a, b int) int {
			return cmp.Compare(tab.Rows[a].ID, tab.Rows[b].ID)
		})
		ReorderTab(tab, indices)
		return
	}

	sorts := withPKTiebreaker(tab.Sorts)

	indices := lo.Times(len(tab.CellRows), func(i int) int { return i })
	slices.SortStableFunc(indices, func(a, b int) int {
		for _, entry := range sorts {
			ca := CellAt(tab, a, entry.Col)
			cb := CellAt(tab, b, entry.Col)

			if ca.Null && cb.Null {
				continue
			}
			if ca.Null {
				return 1
			}
			if cb.Null {
				return -1
			}

			c := CompareCells(tab, entry.Col, a, b)
			if c == 0 {
				continue
			}
			if entry.Dir == SortDesc {
				return -c
			}
			return c
		}
		return 0
	})

	ReorderTab(tab, indices)
}

// CompareCells compares two cells in the same column, using numeric comparison
// for integer/real columns and case-insensitive string comparison otherwise.
func CompareCells(tab *Tab, col, a, b int) int {
	va := CellValueAt(tab, a, col)
	vb := CellValueAt(tab, b, col)

	if va == vb {
		return 0
	}

	kind := CellText
	if col >= 0 && col < len(tab.Specs) {
		kind = tab.Specs[col].Kind
	}

	switch kind {
	case CellInteger, CellReal, CellReadonly:
		na, errA := strconv.ParseFloat(va, 64)
		nb, errB := strconv.ParseFloat(vb, 64)
		if errA != nil || errB != nil {
			return cmp.Compare(strings.ToLower(va), strings.ToLower(vb))
		}
		return cmp.Compare(na, nb)
	default:
		return cmp.Compare(strings.ToLower(va), strings.ToLower(vb))
	}
}

// withPKTiebreaker appends column 0 as a tiebreaker if it's not already
// in the sort list. This ensures deterministic ordering.
func withPKTiebreaker(sorts []SortEntry) []SortEntry {
	if slices.ContainsFunc(sorts, func(e SortEntry) bool { return e.Col == 0 }) {
		return sorts
	}
	return append(sorts, SortEntry{Col: 0, Dir: SortAsc})
}

// CellValueAt returns the trimmed string value of a cell.
func CellValueAt(tab *Tab, row, col int) string {
	return strings.TrimSpace(CellAt(tab, row, col).Value)
}

// CellAt returns the cell at the given row and column, or an empty Cell
// if the indices are out of bounds.
func CellAt(tab *Tab, row, col int) Cell {
	if row < 0 || row >= len(tab.CellRows) {
		return Cell{}
	}
	cells := tab.CellRows[row]
	if col < 0 || col >= len(cells) {
		return Cell{}
	}
	return cells[col]
}

// ReorderTab reorders all row arrays in the tab according to the given indices.
// indices[i] is the source index for the i-th position in the result.
func ReorderTab(tab *Tab, indices []int) {
	n := len(indices)
	newCellRows := make([][]Cell, n)
	newMeta := make([]RowMeta, n)
	newFullCellRows := make([][]Cell, n)
	newFullMeta := make([]RowMeta, n)
	tableRows := tab.Table.Rows()
	newTableRows := make([]table.Row, n)
	newFullRows := make([]table.Row, n)

	for i, idx := range indices {
		newCellRows[i] = tab.CellRows[idx]
		newMeta[i] = tab.Rows[idx]
		if idx < len(tableRows) {
			newTableRows[i] = tableRows[idx]
		}
		if idx < len(tab.FullCellRows) {
			newFullCellRows[i] = tab.FullCellRows[idx]
		}
		if idx < len(tab.FullMeta) {
			newFullMeta[i] = tab.FullMeta[idx]
		}
		if idx < len(tab.FullRows) {
			newFullRows[i] = tab.FullRows[idx]
		}
	}

	tab.CellRows = newCellRows
	tab.Rows = newMeta
	tab.Table.SetRows(newTableRows)
	tab.FullCellRows = newFullCellRows
	tab.FullMeta = newFullMeta
	tab.FullRows = newFullRows
}
