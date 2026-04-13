package tabstate

// filter.go — per-column pin filters: toggle a cell value as a filter,
// recompute visible rows, and render filter indicators in headers.

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/samber/lo"
)

// HasPins returns true if the tab has any active pin filters.
func HasPins(tab *Tab) bool {
	return tab != nil && len(tab.Pins) > 0
}

// TogglePin adds or removes a value from the pin set for a column.
// Returns true if the value was added (pinned), false if removed (unpinned).
func TogglePin(tab *Tab, col int, value string) bool {
	key := strings.ToLower(strings.TrimSpace(value))
	for i := range tab.Pins {
		if tab.Pins[i].Col != col {
			continue
		}
		if tab.Pins[i].Values[key] {
			delete(tab.Pins[i].Values, key)
			if len(tab.Pins[i].Values) == 0 {
				tab.Pins = append(tab.Pins[:i], tab.Pins[i+1:]...)
			}
			return false
		}
		tab.Pins[i].Values[key] = true
		return true
	}
	tab.Pins = append(tab.Pins, FilterPin{
		Col:    col,
		Values: map[string]bool{key: true},
	})
	return true
}

// ClearPins removes all pin filters and resets filter state flags.
func ClearPins(tab *Tab) {
	tab.Pins = nil
	tab.FilterActive = false
	tab.FilterInverted = false
}

// CellDisplayValue returns the normalized display value for pin matching.
// NULL cells return [NullPinKey]; non-null cells are lowercased and trimmed.
func CellDisplayValue(c Cell) string {
	if c.Null {
		return NullPinKey
	}
	return strings.ToLower(strings.TrimSpace(c.Value))
}

// MatchesAllPins returns true if a row matches all pin filters (AND semantics).
// Each pin's values use OR semantics within the same column.
func MatchesAllPins(cellRow []Cell, pins []FilterPin) bool {
	return lo.EveryBy(pins, func(pin FilterPin) bool {
		return pin.Col < len(cellRow) && pin.Values[CellDisplayValue(cellRow[pin.Col])]
	})
}

// ApplyRowFilter recomputes CellRows based on pins and filter settings.
//
// When no pins are set, all rows are shown. When FilterActive is true,
// non-matching rows are excluded. When FilterActive is false (preview mode),
// non-matching rows are dimmed but still visible. FilterInverted flips the
// match logic.
func ApplyRowFilter(tab *Tab) {
	if len(tab.Pins) == 0 {
		tab.Rows = CopyMeta(tab.FullMeta)
		tab.CellRows = tab.FullCellRows
		tab.Table.SetRows(tab.FullRows)
		SnapshotPostPin(tab)
		return
	}

	if tab.FilterActive {
		var filteredRows []table.Row
		var filteredMeta []RowMeta
		var filteredCells [][]Cell
		for i := range tab.FullCellRows {
			if MatchesAllPins(tab.FullCellRows[i], tab.Pins) != tab.FilterInverted {
				filteredRows = append(filteredRows, tab.FullRows[i])
				filteredMeta = append(filteredMeta, tab.FullMeta[i])
				filteredCells = append(filteredCells, tab.FullCellRows[i])
			}
		}
		tab.Rows = filteredMeta
		tab.CellRows = filteredCells
		tab.Table.SetRows(filteredRows)
		SnapshotPostPin(tab)
		return
	}

	meta := CopyMeta(tab.FullMeta)
	for i := range tab.FullCellRows {
		if MatchesAllPins(tab.FullCellRows[i], tab.Pins) == tab.FilterInverted {
			meta[i].Dimmed = true
		}
	}
	tab.Rows = meta
	tab.CellRows = tab.FullCellRows
	tab.Table.SetRows(tab.FullRows)
	SnapshotPostPin(tab)
}

// SnapshotPostPin saves the current post-pin-filter state for the search
// filter to read from. Call this after any filter change.
func SnapshotPostPin(tab *Tab) {
	tab.PostPinRows = tab.Table.Rows()
	tab.PostPinMeta = CopyMeta(tab.Rows)
	tab.PostPinCellRows = tab.CellRows
}

// CopyMeta returns a shallow copy of a RowMeta slice.
func CopyMeta(src []RowMeta) []RowMeta {
	return slices.Clone(src)
}
