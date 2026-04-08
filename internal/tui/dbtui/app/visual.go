package app

// visual.go — visual (multi-row select) mode: range selection, yank-to-
// clipboard, and state restoration after database mutations.

import (
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
)

// restoreTabState carries over sorts, filters, cursor, and column position
// from an old tab to a freshly-built newTab after a database mutation.
func restoreTabState(newTab *Tab, old *Tab, oldCursor int) {
	newTab.Sorts = old.Sorts
	tabstate.ApplySorts(newTab)

	newTab.Pins = old.Pins
	newTab.FilterActive = old.FilterActive
	newTab.FilterInverted = old.FilterInverted
	if len(newTab.Pins) > 0 {
		tabstate.ApplyRowFilter(newTab)
	}

	newTab.Table.SetHeight(old.Table.Height())

	// Restore column cursor.
	if old.ColCursor < len(newTab.Specs) {
		newTab.ColCursor = old.ColCursor
	}
	newTab.ViewOffset = old.ViewOffset

	newTab.Table.SetCursor(clampCursor(oldCursor, len(newTab.CellRows)))
}

// enterVisualMode switches to visual mode if the current tab is writable.
func (m *Model) enterVisualMode() {
	if m.currentTabReadOnly() {
		m.setStatusError("Visual mode unavailable: read-only table")
		return
	}
	m.mode = modeVisual
	m.visual = &visualState{
		Anchor:   -1,
		Selected: map[int]bool{},
	}
}

// exitVisualMode returns to normal mode and clears selection state.
func (m *Model) exitVisualMode() {
	m.mode = modeNormal
	m.visual = nil
}

// resetVisualSelection clears the selection but stays in visual mode.
func (m *Model) resetVisualSelection() {
	if m.visual != nil {
		m.visual.Anchor = -1
		m.visual.Selected = map[int]bool{}
		m.visual.CachedSet = nil
	}
}

// effectiveVisualSelection returns the sorted list of selected row indices.
// If nothing is explicitly selected (no space-toggles and no anchor range),
// returns the cursor row only.
func (m *Model) effectiveVisualSelection() []int {
	tab := m.effectiveTab()
	if tab == nil || m.visual == nil {
		return nil
	}

	seen := map[int]bool{}

	// Add individually toggled rows.
	for idx := range m.visual.Selected {
		seen[idx] = true
	}

	// Add anchor range.
	cursor := tab.Table.Cursor()
	if m.visual.Anchor >= 0 {
		lo, hi := m.visual.Anchor, cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			seen[i] = true
		}
	}

	// If nothing selected, use cursor row.
	if len(seen) == 0 {
		return []int{cursor}
	}

	result := slices.Sorted(maps.Keys(seen))
	return result
}

// visualSelectionSet returns a set of all selected row indices for rendering.
// Results are cached and only recomputed when the cursor moves or selection changes.
func (m *Model) visualSelectionSet() map[int]bool {
	tab := m.effectiveTab()
	if tab == nil || m.visual == nil {
		return nil
	}
	cursor := tab.Table.Cursor()
	if m.visual.CachedSet != nil && m.visual.CachedCursor == cursor {
		return m.visual.CachedSet
	}
	sel := m.effectiveVisualSelection()
	set := make(map[int]bool, len(sel))
	for _, idx := range sel {
		set[idx] = true
	}
	m.visual.CachedSet = set
	m.visual.CachedCursor = cursor
	return set
}

// explicitVisualSelectionCount returns the number of rows the user has
// explicitly selected (space-toggles + anchor range). Unlike
// effectiveVisualSelection it does NOT fall back to the cursor row, so the
// count is 0 when no selection action has been taken.
func (m *Model) explicitVisualSelectionCount() int {
	tab := m.effectiveTab()
	if tab == nil || m.visual == nil {
		return 0
	}

	seen := map[int]bool{}
	for idx := range m.visual.Selected {
		seen[idx] = true
	}
	if m.visual.Anchor >= 0 {
		cursor := tab.Table.Cursor()
		lo, hi := m.visual.Anchor, cursor
		if lo > hi {
			lo, hi = hi, lo
		}
		for i := lo; i <= hi; i++ {
			seen[i] = true
		}
	}
	return len(seen)
}

// visualYank copies selected rows into the internal clipboard.
func (m *Model) visualYank() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	sel := m.effectiveVisualSelection()
	m.clipboard = make([][]cell, 0, len(sel))
	for _, idx := range sel {
		if idx < len(tab.CellRows) {
			// Deep-copy the row.
			row := make([]cell, len(tab.CellRows[idx]))
			copy(row, tab.CellRows[idx])
			m.clipboard = append(m.clipboard, row)
		}
	}
}

// formatRowsTSV formats selected rows as TSV with a header line.
func formatRowsTSV(tab *Tab, selection []int) string {
	var b strings.Builder
	// Header.
	for i, spec := range tab.Specs {
		if i > 0 {
			b.WriteByte('\t')
		}
		b.WriteString(spec.Title)
	}
	b.WriteByte('\n')
	// Data rows.
	for _, idx := range selection {
		if idx >= len(tab.CellRows) {
			continue
		}
		row := tab.CellRows[idx]
		for i, c := range row {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(c.Value)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// visualYankSystem copies selected rows to the system clipboard as TSV via pbcopy.
func (m *Model) visualYankSystem() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	sel := m.effectiveVisualSelection()
	tsv := formatRowsTSV(tab, sel)

	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(tsv)
	if err := cmd.Run(); err != nil {
		m.setStatusError(fmt.Sprintf("Copy to clipboard failed: %v", err))
		return
	}
	m.setStatusInfo(fmt.Sprintf("Copied %d row(s) to system clipboard", len(sel)))
}

// visualDelete deletes selected rows from the database and rebuilds the tab.
func (m *Model) visualDelete() tea.Cmd {
	tab := m.effectiveTab()
	if tab == nil {
		return nil
	}
	sel := m.effectiveVisualSelection()
	if len(sel) == 0 {
		return nil
	}

	// Build identifiers.
	ids := make([]data.RowIdentifier, 0, len(sel))
	for _, idx := range sel {
		if idx >= len(tab.Rows) {
			continue
		}
		rm := tab.Rows[idx]
		ids = append(ids, data.RowIdentifier{
			RowID: rm.RowID,
		})
	}

	deleted, err := m.store.DeleteRows(tab.Name, ids)
	if err != nil {
		m.setStatusError(fmt.Sprintf("Delete failed: %v", err))
		return nil
	}

	// Rebuild the tab from database.
	if err := m.rebuildTab(m.active); err != nil {
		m.setStatusError(err.Error())
		return nil
	}
	m.setStatusInfo(fmt.Sprintf("Deleted %d row(s)", deleted))
	m.resetVisualSelection()
	return nil
}

// visualCut yanks then deletes.
func (m *Model) visualCut() tea.Cmd {
	m.visualYank()
	return m.visualDelete()
}

// visualPaste inserts clipboard rows into the database and rebuilds the tab.
func (m *Model) visualPaste(below bool) tea.Cmd {
	tab := m.effectiveTab()
	if tab == nil {
		return nil
	}
	if len(m.clipboard) == 0 {
		m.setStatusError("Clipboard is empty")
		return nil
	}

	// Build column names (skip readonly columns).
	var columns []string
	var colIndices []int
	for i, spec := range tab.Specs {
		if spec.Kind != cellReadonly {
			columns = append(columns, spec.DBName)
			colIndices = append(colIndices, i)
		}
	}

	// Build row data from clipboard.
	rows := make([][]string, 0, len(m.clipboard))
	for _, clipRow := range m.clipboard {
		row := make([]string, len(colIndices))
		for j, ci := range colIndices {
			if ci < len(clipRow) {
				row[j] = clipRow[ci].Value
			}
		}
		rows = append(rows, row)
	}

	if err := m.store.InsertRows(tab.Name, columns, rows); err != nil {
		m.setStatusError(fmt.Sprintf("Paste failed: %v", err))
		return nil
	}

	// Rebuild.
	if err := m.rebuildTab(m.active); err != nil {
		m.setStatusError(err.Error())
		return nil
	}
	m.setStatusInfo(fmt.Sprintf("Pasted %d row(s)", len(m.clipboard)))
	m.resetVisualSelection()
	return nil
}

// visualExportCSV exports selected rows to a CSV file in the current directory.
func (m *Model) visualExportCSV() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	sel := m.effectiveVisualSelection()
	if len(sel) == 0 {
		return
	}

	// Build header and rows from selection.
	header := make([]string, len(tab.Specs))
	for i, spec := range tab.Specs {
		header[i] = spec.Title
	}
	rows := make([][]string, 0, len(sel))
	for _, idx := range sel {
		if idx >= len(tab.CellRows) {
			continue
		}
		row := make([]string, len(tab.CellRows[idx]))
		for i, c := range tab.CellRows[idx] {
			row[i] = c.Value
		}
		rows = append(rows, row)
	}

	csvPath := fmt.Sprintf("%s_selection.csv", tab.Name)
	if err := data.WriteRowsCSV(csvPath, header, rows); err != nil {
		m.setStatusError(fmt.Sprintf("Export failed: %v", err))
		return
	}
	m.setStatusInfo(fmt.Sprintf("Exported %d row(s) → %s", len(rows), csvPath))
}

// handleVisualKey dispatches key events in visual mode.
func (m *Model) handleVisualKey(key tea.KeyPressMsg) tea.Cmd {
	tab := m.effectiveTab()
	if tab == nil {
		return nil
	}

	k := key.String()
	switch k {
	// Row navigation (no selection side-effect).
	case keyJ, keyDown:
		m.cursorDown(tab)
	case keyK, keyUp:
		m.cursorUp(tab)
	case keyG:
		tab.Table.SetCursor(0)
	case keyShiftG:
		if n := len(tab.CellRows); n > 0 {
			tab.Table.SetCursor(n - 1)
		}

	// Column navigation (horizontal scroll only).
	case keyH, keyLeft:
		m.colLeft(tab)
	case keyL, keyRight:
		m.colRight(tab)

	// Toggle individual row selection.
	case keySpace:
		cursor := tab.Table.Cursor()
		if m.visual.Selected[cursor] {
			delete(m.visual.Selected, cursor)
		} else {
			m.visual.Selected[cursor] = true
		}
		m.visual.CachedSet = nil

	// Shift+J: extend selection downward.
	case keyShiftJ:
		if m.visual.Anchor < 0 {
			m.visual.Anchor = tab.Table.Cursor()
		}
		m.cursorDown(tab)

	// Shift+K: extend selection upward.
	case keyShiftK:
		if m.visual.Anchor < 0 {
			m.visual.Anchor = tab.Table.Cursor()
		}
		m.cursorUp(tab)

	// Operations.
	case keyD:
		return m.visualDelete()
	case keyX:
		return m.visualCut()
	case keyY, keyC:
		m.visualYank()
		sel := m.effectiveVisualSelection()
		m.setStatusInfo(fmt.Sprintf("Yanked %d row(s)", len(sel)))
	case keyShiftY, keyShiftC:
		m.visualYankSystem()
	case keyE:
		m.visualExportCSV()
	case keyP:
		return m.visualPaste(true)
	case keyShiftP:
		return m.visualPaste(false)
	}
	return nil
}
