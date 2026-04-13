package app

// column_ops.go — column rename and drop operations.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/uikit"
)

// openColumnRename opens the column rename overlay for the current column.
func (m *Model) openColumnRename() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	if m.currentTabReadOnly() {
		m.setStatusError("Cannot rename column: read-only table")
		return
	}
	col := tab.ColCursor
	if col < 0 || col >= len(tab.Specs) {
		return
	}

	ti := textinput.New()
	ti.SetValue(tab.Specs[col].DBName)
	ti.Focus()
	ti.CharLimit = 64
	ti.Prompt = ""

	m.columnRename = &columnRenameState{
		Input:     ti,
		OldName:   tab.Specs[col].DBName,
		TableName: tab.Name,
		ColIdx:    col,
	}
}

// handleColumnRenameKey handles key events in the column rename overlay.
func (m *Model) handleColumnRenameKey(key tea.KeyPressMsg) tea.Cmd {
	cr := m.columnRename
	if cr == nil {
		return nil
	}

	switch key.String() {
	case keyEsc:
		m.columnRename = nil
		return nil
	case keyEnter:
		return m.commitColumnRename()
	}

	var cmd tea.Cmd
	cr.Input, cmd = cr.Input.Update(key)
	return cmd
}

// commitColumnRename validates and executes the rename.
func (m *Model) commitColumnRename() tea.Cmd {
	cr := m.columnRename
	if cr == nil {
		return nil
	}

	newName := cr.Input.Value()
	m.columnRename = nil

	if newName == cr.OldName {
		return nil
	}

	if !data.IsSafeIdentifier(newName) {
		m.setStatusError(fmt.Sprintf("Invalid column name: %q", newName))
		return nil
	}

	store := m.concreteStore()
	if store == nil {
		m.setStatusError("Column rename not supported for this backend")
		return nil
	}

	if err := store.RenameColumn(cr.TableName, cr.OldName, newName); err != nil {
		m.setStatusError(fmt.Sprintf("Rename failed: %v", err))
		return nil
	}

	// Rebuild the tab from database.
	if err := m.rebuildTab(m.active); err != nil {
		m.setStatusError(err.Error())
		return nil
	}
	m.setStatusInfo(fmt.Sprintf("Renamed %q → %q", cr.OldName, newName))
	return nil
}

// dropColumn drops the current column from the table.
func (m *Model) dropColumn() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	if m.currentTabReadOnly() {
		m.setStatusError("Cannot drop column: read-only table")
		return
	}

	col := tab.ColCursor
	if col < 0 || col >= len(tab.Specs) {
		return
	}

	store := m.concreteStore()
	if store == nil {
		m.setStatusError("Column drop not supported for this backend")
		return
	}

	colName := tab.Specs[col].DBName
	if err := store.DropColumn(tab.Name, colName); err != nil {
		m.setStatusError(fmt.Sprintf("Drop failed: %v", err))
		return
	}

	// Rebuild the tab from database.
	if err := m.rebuildTab(m.active); err != nil {
		m.setStatusError(err.Error())
		return
	}
	m.setStatusInfo(fmt.Sprintf("Dropped column %q", colName))
}

// buildColumnRenameOverlay renders the column rename overlay.
func (m *Model) buildColumnRenameOverlay() string {
	cr := m.columnRename
	if cr == nil {
		return ""
	}

	contentW := uikit.OverlayWidth(m.width, 20, 60)

	var b strings.Builder
	b.WriteString(m.overlayHeader("Rename Column"))
	b.WriteString(cr.Input.View())
	b.WriteString("\n\n")
	hints := joinWithSeparator(
		m.helpSeparator(),
		m.helpItem(keyEnter, "rename"),
		m.helpItem(keyEsc, "cancel"),
	)
	b.WriteString(hints)

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}
