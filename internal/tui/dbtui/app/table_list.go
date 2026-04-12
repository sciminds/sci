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
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
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

	k := msg.String()
	switch k {
	case keyEsc, keyT, keyQ:
		m.tableList = nil
		return nil

	case keyJ, keyDown:
		if tl.Cursor < len(tl.Tables)-1 {
			tl.Cursor++
			tl.Status = ""
		}

	case keyK, keyUp:
		if tl.Cursor > 0 {
			tl.Cursor--
			tl.Status = ""
		}

	case keyEnter:
		// Switch to the selected table's tab.
		if len(tl.Tables) > 0 {
			name := tl.Tables[tl.Cursor].Name
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
		if len(tl.Tables) > 0 && tl.Tables[tl.Cursor].IsView {
			tl.Status = "Cannot delete a view"
		} else if len(tl.Tables) > 0 && tl.Tables[tl.Cursor].IsVirtual {
			tl.Status = "Cannot delete a virtual table"
		} else {
			m.tableListDelete()
		}

	case keyE:
		m.tableListExport()

	case keyA:
		return m.tableListAdd()

	case keyR:
		if len(tl.Tables) > 0 && tl.Tables[tl.Cursor].IsView {
			tl.Status = "Cannot rename a view"
		} else if len(tl.Tables) > 0 && tl.Tables[tl.Cursor].IsVirtual {
			tl.Status = "Cannot rename a virtual table"
		} else {
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
	if tl == nil || len(tl.Tables) == 0 {
		return
	}
	name := tl.Tables[tl.Cursor].Name
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

	oldName := tl.Tables[tl.Cursor].Name
	newName := tl.RenameInput.Value()

	// Exit rename mode.
	tl.Renaming = false
	tl.RenameInput = textinput.Model{}

	// No-op if name unchanged.
	if newName == oldName {
		return
	}

	// Validate new name.
	if !data.IsSafeIdentifier(newName) {
		tl.Status = fmt.Sprintf("Invalid name: %q (alphanumerics, underscores, and spaces only)", newName)
		return
	}

	// Check for collision.
	for i, e := range tl.Tables {
		if i != tl.Cursor && e.Name == newName {
			tl.Status = fmt.Sprintf("Table %q already exists", newName)
			return
		}
	}

	if err := m.store.RenameTable(oldName, newName); err != nil {
		tl.Status = fmt.Sprintf("Rename failed: %v", err)
		return
	}

	// Update overlay list.
	tl.Tables[tl.Cursor].Name = newName

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
	if tl == nil || len(tl.Tables) == 0 {
		return
	}

	entry := tl.Tables[tl.Cursor]

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

	// Remove from overlay list.
	tl.Tables = append(tl.Tables[:tl.Cursor], tl.Tables[tl.Cursor+1:]...)
	if tl.Cursor >= len(tl.Tables) {
		tl.Cursor = len(tl.Tables) - 1
	}
	if tl.Cursor < 0 {
		tl.Cursor = 0
	}

	tl.Status = fmt.Sprintf("Dropped %q", entry.Name)
}

// tableListExport exports the selected table to a CSV file in the current directory.
func (m *Model) tableListExport() {
	tl := m.tableList
	if tl == nil || len(tl.Tables) == 0 {
		return
	}

	entry := tl.Tables[tl.Cursor]
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
	if tl == nil || len(tl.Tables) == 0 {
		return
	}

	entry := tl.Tables[tl.Cursor]
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
	tl.Tables[tl.Cursor] = entry

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
