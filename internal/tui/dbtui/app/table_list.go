package app

// table_list.go — table-switcher overlay: lists all tables in the database,
// lets the user jump to one, and provides table-level operations (delete,
// export, dedup, rename). Sub-features live in sibling files:
//
//   - table_list_browse.go  — file browser for importing CSV/TSV
//   - table_list_create.go  — create empty table / derive table from SQL
//   - table_list_render.go  — overlay rendering and action hints

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/store"
)

// toggleTableList opens or closes the table list overlay.
func (m *Model) toggleTableList() {
	if m.tableList != nil {
		m.tableList = nil
		return
	}
	m.openTableList()
}

// openTableList builds the table list state from the current database.
func (m *Model) openTableList() {
	entries, err := m.buildTableListEntries()
	if err != nil {
		m.setStatusError(fmt.Sprintf("Table list: %v", err))
		return
	}
	cursor := 0
	// Position cursor on the currently active tab.
	for i, e := range entries {
		if m.active < len(m.tabs) && e.Name == m.tabs[m.active].Name {
			cursor = i
			break
		}
	}
	m.tableList = &tableListState{
		Tables: entries,
		Cursor: cursor,
	}
}

// buildTableListEntries queries the store for table names, row counts, and column counts.
func (m *Model) buildTableListEntries() ([]tableListEntry, error) {
	summaries, err := m.store.TableSummaries()
	if err != nil {
		return nil, err
	}
	entries := make([]tableListEntry, 0, len(summaries))
	for _, s := range summaries {
		entries = append(entries, tableListEntry{
			Name:      s.Name,
			Rows:      s.Rows,
			Columns:   s.Columns,
			IsView:    m.viewLister != nil && m.viewLister.IsView(s.Name),
			IsVirtual: m.virtualLister != nil && m.virtualLister.IsVirtual(s.Name),
		})
	}
	return entries, nil
}

// ── Filtering ─────────────────────────────────────────────────────────────

// tableMatch is one entry in the visible (filtered) list: an index into
// tableListState.Tables plus the matched rune positions for highlighting.
type tableMatch struct {
	Index     int
	Positions []int
}

// visibleMatches returns the entries that match the active fuzzy Query, in
// rank order, each paired with the rune positions that matched. With no
// active query every table is returned in its original order.
func (tl *tableListState) visibleMatches() []tableMatch {
	if tl.Query == "" {
		return lo.Map(tl.Tables, func(_ tableListEntry, i int) tableMatch {
			return tableMatch{Index: i}
		})
	}
	names := lo.Map(tl.Tables, func(e tableListEntry, _ int) string {
		return e.Name
	})
	return lo.Map(fuzzy.Find(tl.Query, names), func(match fuzzy.Match, _ int) tableMatch {
		return tableMatch{Index: match.Index, Positions: match.MatchedIndexes}
	})
}

// selectedIndex maps the visible cursor to an index into tl.Tables, honoring
// the active filter. Returns -1 when nothing is visible.
func (tl *tableListState) selectedIndex() int {
	vis := tl.visibleMatches()
	if len(vis) == 0 {
		return -1
	}
	c := min(tl.Cursor, len(vis)-1)
	return vis[c].Index
}

// clampCursor keeps Cursor within the bounds of the current visible list.
func (tl *tableListState) clampCursor() {
	n := len(tl.visibleMatches())
	if tl.Cursor >= n {
		tl.Cursor = n - 1
	}
	if tl.Cursor < 0 {
		tl.Cursor = 0
	}
}

// startFilter focuses the / filter input, seeded with any existing query so
// the user can refine it.
func (m *Model) startFilter() {
	tl := m.tableList
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetValue(tl.Query)
	ti.CursorEnd()
	ti.Focus()
	tl.Filtering = true
	tl.FilterInput = ti
	tl.Status = ""
}

// clearFilter drops the active filter and resets the cursor.
func (tl *tableListState) clearFilter() {
	tl.Filtering = false
	tl.Query = ""
	tl.FilterInput = textinput.Model{}
	tl.Cursor = 0
}

// handleTableListFilterKey handles key events while the / filter is focused.
func (m *Model) handleTableListFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	switch msg.String() {
	case keyEsc:
		// Cancel: discard the query and restore the full list.
		tl.clearFilter()
		return nil
	case keyEnter:
		// Commit: keep the query applied but stop capturing keystrokes so
		// normal navigation (j/k/g/G/enter) resumes over the filtered list.
		tl.Filtering = false
		tl.clampCursor()
		return nil
	}

	var cmd tea.Cmd
	tl.FilterInput, cmd = tl.FilterInput.Update(msg)
	tl.Query = tl.FilterInput.Value()
	tl.Cursor = 0 // re-rank resets the selection to the top match
	return cmd
}

// handleTableListKey dispatches key events when the table list overlay is active.
func (m *Model) handleTableListKey(msg tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	// If adding, delegate to file picker handler.
	if tl.Adding {
		return m.handleTableListAddKey(msg)
	}

	// If creating, delegate to create-table key handler.
	if tl.Creating {
		return m.handleTableListCreateKey(msg)
	}

	// If deriving, delegate to derive key handler.
	if tl.Deriving {
		return m.handleTableListDeriveKey(msg)
	}

	// If renaming, delegate to rename key handler.
	if tl.Renaming {
		return m.handleTableListRenameKey(msg)
	}

	// If filtering, delegate to the / filter key handler.
	if tl.Filtering {
		return m.handleTableListFilterKey(msg)
	}

	visible := len(tl.visibleMatches())
	k := msg.String()
	switch k {
	case keyEsc:
		// First Esc clears an active filter; a second one closes the overlay.
		if tl.Query != "" {
			tl.clearFilter()
			return nil
		}
		m.tableList = nil
		return nil

	case keyT, keyQ:
		m.tableList = nil
		return nil

	case keySlash:
		m.startFilter()

	case keyJ, keyDown:
		if tl.Cursor < visible-1 {
			tl.Cursor++
			tl.Status = ""
		}

	case keyK, keyUp:
		if tl.Cursor > 0 {
			tl.Cursor--
			tl.Status = ""
		}

	case keyG:
		tl.Cursor = 0
		tl.Status = ""

	case keyShiftG:
		if visible > 0 {
			tl.Cursor = visible - 1
			tl.Status = ""
		}

	case keyEnter:
		// Switch to the selected table's tab.
		if idx := tl.selectedIndex(); idx >= 0 {
			name := tl.Tables[idx].Name
			for i, tab := range m.tabs {
				if tab.Name == name {
					cmd := m.switchToTab(i)
					m.tableList = nil
					return cmd
				}
			}
			m.tableList = nil
		}

	case keyD:
		idx := tl.selectedIndex()
		switch {
		case idx < 0:
		case tl.Tables[idx].IsView:
			tl.Status = "Cannot delete a view"
		case tl.Tables[idx].IsVirtual:
			tl.Status = "Cannot delete a virtual table"
		default:
			m.tableListDelete()
		}

	case keyE:
		m.tableListExport()

	case keyA:
		return m.tableListAdd()

	case keyR:
		idx := tl.selectedIndex()
		switch {
		case idx < 0:
		case tl.Tables[idx].IsView:
			tl.Status = "Cannot rename a view"
		case tl.Tables[idx].IsVirtual:
			tl.Status = "Cannot rename a virtual table"
		default:
			m.tableListStartRename()
		}

	case keyC:
		m.tableListStartCreate()

	case keyU:
		m.tableListDedup()

	case keyS:
		m.tableListStartDerive()
	}

	return nil
}

// ── Rename ──────────────────────────────────────────────────────────────────

// tableListStartRename enters rename mode for the selected table.
func (m *Model) tableListStartRename() {
	tl := m.tableList
	if tl == nil {
		return
	}
	idx := tl.selectedIndex()
	if idx < 0 {
		return
	}
	name := tl.Tables[idx].Name
	ti := textinput.New()
	ti.SetValue(name)
	ti.Focus()
	ti.CharLimit = 64
	ti.Prompt = ""
	tl.Renaming = true
	tl.RenameInput = ti
	tl.Status = ""
}

// handleTableListRenameKey handles key events while renaming a table.
func (m *Model) handleTableListRenameKey(key tea.KeyPressMsg) tea.Cmd {
	tl := m.tableList
	if tl == nil {
		return nil
	}

	switch key.String() {
	case keyEsc:
		tl.Renaming = false
		tl.RenameInput = textinput.Model{}
		return nil
	case keyEnter:
		m.tableListCommitRename()
		return nil
	}

	// Delegate everything else to textinput.
	var cmd tea.Cmd
	tl.RenameInput, cmd = tl.RenameInput.Update(key)
	return cmd
}

// tableListCommitRename validates and executes the rename.
func (m *Model) tableListCommitRename() {
	tl := m.tableList
	if tl == nil || !tl.Renaming {
		return
	}

	idx := tl.selectedIndex()
	if idx < 0 {
		tl.Renaming = false
		tl.RenameInput = textinput.Model{}
		return
	}
	oldName := tl.Tables[idx].Name
	newName := tl.RenameInput.Value()

	// Exit rename mode.
	tl.Renaming = false
	tl.RenameInput = textinput.Model{}

	// No-op if name unchanged.
	if newName == oldName {
		return
	}

	// Validate new name.
	if !store.IsSafeIdentifier(newName) {
		tl.Status = fmt.Sprintf("Invalid name: %q (alphanumerics, underscores, and spaces only)", newName)
		return
	}

	// Check for collision.
	for i, e := range tl.Tables {
		if i != idx && e.Name == newName {
			tl.Status = fmt.Sprintf("Table %q already exists", newName)
			return
		}
	}

	if err := m.store.RenameTable(oldName, newName); err != nil {
		tl.Status = fmt.Sprintf("Rename failed: %v", err)
		return
	}

	// Update overlay list.
	tl.Tables[idx].Name = newName

	// Update in-memory tab.
	for i, tab := range m.tabs {
		if tab.Name == oldName {
			m.tabs[i].Name = newName
			break
		}
	}

	tl.Status = fmt.Sprintf("Renamed %q → %q", oldName, newName)
}

// ── Delete / Export / Dedup ─────────────────────────────────────────────────

// tableListDelete drops the selected table and removes it from the in-memory state.
func (m *Model) tableListDelete() {
	tl := m.tableList
	if tl == nil {
		return
	}
	idx := tl.selectedIndex()
	if idx < 0 {
		return
	}

	entry := tl.Tables[idx]

	if err := m.store.DropTable(entry.Name); err != nil {
		tl.Status = fmt.Sprintf("Drop failed: %v", err)
		return
	}

	// Remove the tab from in-memory state (no full rebuild needed).
	for i, tab := range m.tabs {
		if tab.Name == entry.Name {
			m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
			// Adjust active tab index.
			if m.active >= len(m.tabs) {
				m.active = len(m.tabs) - 1
			}
			if m.active < 0 {
				m.active = 0
			}
			break
		}
	}

	// Remove from overlay list, then keep the cursor within the (possibly
	// filtered) visible range.
	tl.Tables = append(tl.Tables[:idx], tl.Tables[idx+1:]...)
	tl.clampCursor()

	tl.Status = fmt.Sprintf("Dropped %q", entry.Name)
}

// tableListExport exports the selected table to a CSV file in the current directory.
func (m *Model) tableListExport() {
	tl := m.tableList
	if tl == nil {
		return
	}
	idx := tl.selectedIndex()
	if idx < 0 {
		return
	}

	entry := tl.Tables[idx]
	csvPath := entry.Name + ".csv"

	if err := m.store.ExportCSV(entry.Name, csvPath); err != nil {
		tl.Status = fmt.Sprintf("Export failed: %v", err)
		return
	}

	tl.Status = fmt.Sprintf("Exported %q → %s", entry.Name, csvPath)
}

// tableListDedup removes duplicate rows from the selected table.
func (m *Model) tableListDedup() {
	tl := m.tableList
	if tl == nil {
		return
	}
	idx := tl.selectedIndex()
	if idx < 0 {
		return
	}

	entry := tl.Tables[idx]
	if entry.IsView {
		tl.Status = "Cannot dedup a view"
		return
	}
	if entry.IsVirtual {
		tl.Status = "Cannot dedup a virtual table"
		return
	}

	store := m.concreteStore()
	if store == nil {
		tl.Status = "Dedup not supported for this backend"
		return
	}

	removed, err := store.DeduplicateTable(entry.Name)
	if err != nil {
		tl.Status = fmt.Sprintf("Dedup failed: %v", err)
		return
	}

	if removed == 0 {
		tl.Status = fmt.Sprintf("%q: no duplicates found", entry.Name)
		return
	}

	// Update row count in the overlay.
	entry.Rows -= int(removed)
	tl.Tables[idx] = entry

	// Rebuild the tab if it exists.
	for i, tab := range m.tabs {
		if tab.Name == entry.Name {
			if err := m.rebuildTab(i); err != nil {
				tl.Status = err.Error()
				return
			}
			break
		}
	}

	tl.Status = fmt.Sprintf("Removed %d duplicate(s) from %q", removed, entry.Name)
}

// ── Fuzzy highlight ─────────────────────────────────────────────────────────

// highlightFuzzyPositions renders text with matched rune positions highlighted.
// Characters at positions in the positions slice are rendered with highlightStyle;
// all other characters are rendered with baseStyle.
func highlightFuzzyPositions(
	text string,
	positions []int,
	baseStyle, highlightStyle lipgloss.Style,
) string {
	if len(positions) == 0 {
		return baseStyle.Render(text)
	}

	posSet := lo.SliceToMap(positions, func(p int) (int, bool) {
		return p, true
	})

	runes := []rune(text)
	var b strings.Builder
	inMatch := false
	var run []rune

	flush := func() {
		if len(run) == 0 {
			return
		}
		if inMatch {
			b.WriteString(highlightStyle.Render(string(run)))
		} else {
			b.WriteString(baseStyle.Render(string(run)))
		}
		run = run[:0]
	}

	for i, r := range runes {
		matched := posSet[i]
		if matched != inMatch {
			flush()
			inMatch = matched
		}
		run = append(run, r)
	}
	flush()

	return b.String()
}
