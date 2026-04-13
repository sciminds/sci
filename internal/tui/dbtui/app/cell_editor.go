package app

// cell_editor.go — modal textarea overlay for editing a single cell value,
// with save-on-enter and cancel-on-escape.

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// Cell editor overlay constants.
const (
	cellEditorMinW       = 20
	cellEditorMaxW       = 80
	cellEditorMinH       = 6 // minimum textarea height
	cellEditorChrome     = 8 // lines consumed by header, hints, padding
	cellEditorWidthInset = 6 // textarea width = overlayContentW - inset
)

func (m *Model) openCellEditor() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	cursor := tab.Table.Cursor()
	col := tab.ColCursor
	if cursor < 0 || cursor >= len(tab.CellRows) {
		m.setStatusError("No row selected")
		return
	}
	if col < 0 || col >= len(tab.Specs) {
		m.setStatusError("No column selected")
		return
	}
	if col >= len(tab.CellRows[cursor]) {
		m.setStatusError("Column out of range")
		return
	}
	c := tab.CellRows[cursor][col]
	if c.Kind == cellReadonly {
		m.setStatusError("Column is read-only")
		return
	}

	value := c.Value
	if c.Null {
		value = ""
	}

	ta := textarea.New()
	ta.SetValue(value)
	ta.Focus()
	ta.CharLimit = 0 // no limit
	ta.SetWidth(ui.OverlayWidth(m.width, cellEditorMinW, cellEditorMaxW) - cellEditorWidthInset)
	taH := m.height - cellEditorChrome
	if taH < cellEditorMinH {
		taH = cellEditorMinH
	}
	ta.SetHeight(taH)
	ta.ShowLineNumbers = false
	styles := ta.Styles()
	styles.Focused.CursorLine = styles.Focused.CursorLine.UnsetBackground()
	styles.Focused.Base = styles.Focused.Base.
		BorderForeground(ui.TUI.Palette().Blue)
	ta.SetStyles(styles)
	ta.Placeholder = "empty (NULL)"

	m.cellEditor = &cellEditorState{
		Editor:    ta,
		Title:     tab.Specs[col].Title,
		Original:  value,
		RowID:     tab.Rows[cursor].RowID,
		ColName:   tab.Specs[col].DBName,
		TableName: tab.Name,
		TabIdx:    m.active,
		RowIdx:    cursor,
		ColIdx:    col,
	}
}

func (m *Model) closeCellEditor() {
	m.cellEditor = nil
}

func (m *Model) saveCellEditor() tea.Cmd {
	ce := m.cellEditor
	if ce == nil {
		return nil
	}

	newValue := ce.Editor.Value()

	// Write to database.
	var dbValue *string
	if newValue == "" {
		dbValue = nil // set NULL for empty string
	} else {
		dbValue = &newValue
	}

	if err := m.store.UpdateCell(ce.TableName, ce.ColName, ce.RowID, nil, dbValue); err != nil {
		m.setStatusError(fmt.Sprintf("Save failed: %v", err))
		return nil
	}

	// Update in-memory cell data.
	tab := &m.tabs[ce.TabIdx]
	isNull := dbValue == nil

	// Update filtered view.
	if ce.RowIdx < len(tab.CellRows) && ce.ColIdx < len(tab.CellRows[ce.RowIdx]) {
		tab.CellRows[ce.RowIdx][ce.ColIdx].Value = newValue
		tab.CellRows[ce.RowIdx][ce.ColIdx].Null = isNull
	}

	// Update full data (pre-filter).
	for i, rm := range tab.FullMeta {
		if rm.RowID == ce.RowID {
			if ce.ColIdx < len(tab.FullCellRows[i]) {
				tab.FullCellRows[i][ce.ColIdx].Value = newValue
				tab.FullCellRows[i][ce.ColIdx].Null = isNull
			}
			if ce.ColIdx < len(tab.FullRows[i]) {
				tab.FullRows[i][ce.ColIdx] = newValue
			}
			break
		}
	}

	// Update table.Row for the visible row.
	tableRows := tab.Table.Rows()
	if ce.RowIdx < len(tableRows) && ce.ColIdx < len(tableRows[ce.RowIdx]) {
		tableRows[ce.RowIdx][ce.ColIdx] = newValue
		tab.Table.SetRows(tableRows)
	}

	tab.InvalidateVP()
	m.setStatusInfo("Saved " + ce.Title)
	m.closeCellEditor()
	return nil
}

func (m *Model) handleCellEditorKey(key tea.KeyPressMsg) tea.Cmd {
	ce := m.cellEditor
	if ce == nil {
		return nil
	}

	switch key.String() {
	case keyEsc:
		m.closeCellEditor()
		return nil
	case keyEnter:
		// Bare enter saves. Shift+enter inserts newline (handled by textarea).
		return m.saveCellEditor()
	}

	// Delegate everything else to textarea.
	var cmd tea.Cmd
	ce.Editor, cmd = ce.Editor.Update(key)
	return cmd
}

func cellEditorIsDirty(ce *cellEditorState) bool {
	return ce.Editor.Value() != ce.Original
}

func (m *Model) buildCellEditorOverlay() string {
	ce := m.cellEditor
	if ce == nil {
		return ""
	}

	contentW := ui.OverlayWidth(m.width, cellEditorMinW, cellEditorMaxW)

	var b strings.Builder

	title := ce.Title
	if cellEditorIsDirty(ce) {
		title += " *"
	}
	b.WriteString(m.overlayHeader(title))

	// Render the textarea (handles cursor, scrolling, multi-line)
	b.WriteString(ce.Editor.View())

	b.WriteString("\n\n")
	hints := joinWithSeparator(
		m.helpSeparator(),
		m.helpItem(keyEnter, "save"),
		m.helpItem("shift+enter", "newline"),
		m.helpItem(keyEsc, "cancel"),
	)
	b.WriteString(hints)

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}
