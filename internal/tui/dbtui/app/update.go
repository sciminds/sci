package app

// update.go — Bubble Tea Update() dispatch: routes messages to the correct
// handler based on type (window resize, async tab load, key, mouse, tick).

import (
	"fmt"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width - ui.OverlayMargin) // account for overlay padding
		m.resizeTables()
		if m.notePreview != nil {
			m.notePreview.Overlay = m.notePreview.Overlay.Resize(msg.Width, msg.Height)
		}
		if ce := m.cellEditor; ce != nil {
			ce.Editor.SetWidth(ui.OverlayWidth(msg.Width, cellEditorMinW, cellEditorMaxW) - cellEditorWidthInset)
			taH := msg.Height - cellEditorChrome
			if taH < cellEditorMinH {
				taH = cellEditorMinH
			}
			ce.Editor.SetHeight(taH)
		}
		if tl := m.tableList; tl != nil && tl.Deriving {
			tl.DeriveSQL.SetWidth(ui.OverlayWidth(msg.Width, tableListMinW, tableListMaxW) - deriveSQLWidthInset)
			taH := msg.Height - deriveSQLChrome
			if taH < deriveSQLMinH {
				taH = deriveSQLMinH
			}
			tl.DeriveSQL.SetHeight(taH)
		}
		return m, nil
	case tabLoadedMsg:
		return m.handleTabLoaded(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	default:
		// Pass through cursor blink, readDir, and other tick messages to active sub-components.
		return m.updateEditors(msg)
	}
}

// updateEditors forwards non-key messages (e.g. cursor blink, readDir) to active sub-components.
func (m *Model) updateEditors(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.cellEditor != nil {
		var cmd tea.Cmd
		m.cellEditor.Editor, cmd = m.cellEditor.Editor.Update(msg)
		return m, cmd
	}
	if m.columnRename != nil {
		var cmd tea.Cmd
		m.columnRename.Input, cmd = m.columnRename.Input.Update(msg)
		return m, cmd
	}
	if m.tableList != nil && m.tableList.Deriving {
		var cmd tea.Cmd
		if m.tableList.DeriveFocus == 0 {
			m.tableList.DeriveSQL, cmd = m.tableList.DeriveSQL.Update(msg)
		} else {
			m.tableList.DeriveName, cmd = m.tableList.DeriveName.Update(msg)
		}
		return m, cmd
	}
	if m.tableList != nil && m.tableList.Renaming {
		var cmd tea.Cmd
		m.tableList.RenameInput, cmd = m.tableList.RenameInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// 1. Quit: ctrl+q/ctrl+c always; q only in normal mode with no overlays.
	if k == keyCtrlQ || k == keyCtrlC {
		return m, tea.Quit
	}
	if k == keyQ && m.mode == modeNormal && !m.hasActiveOverlay() {
		return m, tea.Quit
	}

	// 2. Overlay keys (help, cell editor, preview, search, table list).
	if handled, cmd := m.dispatchOverlayKey(key); handled {
		return m, cmd
	}

	// 3. Esc / n: exit modes.
	if k == keyEsc || (k == keyN && (m.mode == modeEdit || m.mode == modeVisual)) {
		if m.mode == modeVisual {
			m.exitVisualMode()
			return m, nil
		}
		if m.mode == modeEdit {
			m.mode = modeNormal
			return m, nil
		}
		m.status = statusMsg{}
		return m, nil
	}

	// 4. Table list toggle (works even with no tabs).
	if k == keyT && m.mode == modeNormal {
		m.toggleTableList()
		return m, nil
	}

	// 5. Empty database: only t and quit keys work.
	if len(m.tabs) == 0 {
		return m, nil
	}

	// 6. Visual mode dispatch.
	if m.mode == modeVisual {
		return m, m.handleVisualKey(key)
	}

	tab := m.effectiveTab()
	if tab == nil {
		return m, nil
	}

	// 7. Shared navigation (works in both normal and edit mode).
	switch k {
	case keyJ, keyDown:
		m.cursorDown(tab)
		return m, nil
	case keyK, keyUp:
		m.cursorUp(tab)
		return m, nil
	case keyG:
		tab.Table.SetCursor(0)
		return m, nil
	case keyShiftG:
		if n := len(tab.CellRows); n > 0 {
			tab.Table.SetCursor(n - 1)
		}
		return m, nil
	case keyU:
		m.halfPageUp(tab)
		return m, nil
	case keyTab:
		return m, m.nextTab()
	case keyShiftTab:
		return m, m.prevTab()
	case keySlash:
		m.openSearch()
		return m, nil
	// Column navigation (shared by normal and edit mode).
	case keyH, keyLeft:
		m.colLeft(tab)
		return m, nil
	case keyL, keyRight:
		m.colRight(tab)
		return m, nil
	case keyCaret:
		tab.ColCursor = m.firstSelectableCol(tab)
		m.updateTabViewport(tab)
		return m, nil
	case keyDollar:
		vis := m.visibleColCount(tab)
		if vis > 0 {
			tab.ColCursor = m.lastSelectableCol(tab)
			m.updateTabViewport(tab)
		}
		return m, nil
	}

	// 8. Mode-specific keys.
	if m.mode == modeEdit {
		if k == keyEnter {
			m.openCellEditor()
			return m, nil
		}
	} else {
		if m.handleNormalModeKey(k, tab) {
			return m, nil
		}
	}

	return m, nil
}

// dispatchOverlayKey processes keys when an overlay is active.
// Returns (true, cmd) if the key was consumed, (false, nil) otherwise.
func (m *Model) dispatchOverlayKey(key tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.helpVisible {
		m.helpVisible = false
		return true, nil
	}
	if m.cellEditor != nil {
		return true, m.handleCellEditorKey(key)
	}
	if m.notePreview != nil {
		if key.String() == keyEsc || key.String() == keyQ {
			m.notePreview = nil
			return true, nil
		}
		var cmd tea.Cmd
		m.notePreview.Overlay, cmd = m.notePreview.Overlay.Update(key)
		return true, cmd
	}
	if m.search != nil && !m.search.Committed {
		return true, m.handleSearchKey(key)
	}
	if m.columnRename != nil {
		return true, m.handleColumnRenameKey(key)
	}
	if m.columnPicker != nil {
		return true, m.handleColumnPickerKey(key)
	}
	if m.tableList != nil {
		return true, m.handleTableListKey(key)
	}
	return false, nil
}

// ── Mouse handling ──────────────────────────────────────────────────────────

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m.handleLeftClick(msg)
		}
	case tea.MouseWheelMsg:
		if msg.Button == tea.MouseWheelUp {
			return m.handleScroll(-1)
		}
		if msg.Button == tea.MouseWheelDown {
			return m.handleScroll(1)
		}
	}
	return m, nil
}

func (m *Model) handleLeftClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.hasActiveOverlay() {
		z := m.zones.Get(zoneOverlay)
		if z != nil && !z.IsZero() && z.InBounds(msg) {
			return m, nil
		}
		m.dismissActiveOverlay()
		return m, nil
	}

	// Tab click.
	if m.mode != modeEdit {
		for i := range m.tabs {
			if m.zones.Get(fmt.Sprintf("%s%d", zoneTab, i)).InBounds(msg) {
				if i != m.active {
					return m, m.switchToTab(i)
				}
				return m, nil
			}
		}
	}

	// Column header click.
	if tab := m.effectiveTab(); tab != nil {
		vp := m.tabViewport(tab)
		for i := range vp.Specs {
			if m.zones.Get(fmt.Sprintf("%s%d", zoneCol, i)).InBounds(msg) {
				if i < len(vp.VisToFull) {
					tab.ColCursor = vp.VisToFull[i]
					m.updateTabViewport(tab)
				}
				return m, nil
			}
		}
	}

	// Row click.
	if tab := m.effectiveTab(); tab != nil {
		total := len(tab.CellRows)
		if total > 0 {
			cursor := tab.Table.Cursor()
			height := tab.Table.Height()
			badges := renderHiddenBadges(tab.Specs, tab.ColCursor)
			if badges != "" {
				height--
			}
			if len(tab.Rows) > 0 {
				height--
			}
			if height < 2 {
				height = 2
			}
			start, end := visibleRange(total, height, cursor)
			for i := start; i < end; i++ {
				if m.zones.Get(fmt.Sprintf("%s%d", zoneRow, i)).InBounds(msg) {
					tab.Table.SetCursor(i)
					m.selectClickedColumn(tab, msg)
					return m, nil
				}
			}
		}
	}

	// Hint clicks.
	if m.zones.Get(zoneHint + "edit").InBounds(msg) {
		if m.mode == modeNormal && !m.currentTabReadOnly() {
			m.mode = modeEdit
		}
	}
	if m.zones.Get(zoneHint + "help").InBounds(msg) {
		m.helpVisible = true
	}

	return m, nil
}

func (m *Model) selectClickedColumn(tab *Tab, msg tea.MouseClickMsg) {
	vp := m.tabViewport(tab)
	for i := range vp.Specs {
		z := m.zones.Get(fmt.Sprintf("%s%d", zoneCol, i))
		if z == nil || z.IsZero() {
			continue
		}
		if msg.X >= z.StartX && msg.X <= z.EndX {
			if i < len(vp.VisToFull) {
				tab.ColCursor = vp.VisToFull[i]
				m.updateTabViewport(tab)
			}
			return
		}
	}
}

func (m *Model) handleScroll(delta int) (tea.Model, tea.Cmd) {
	tab := m.effectiveTab()
	if tab == nil {
		return m, nil
	}
	total := len(tab.CellRows)
	if total == 0 {
		return m, nil
	}
	tab.Table.SetCursor(clampCursor(tab.Table.Cursor()+delta, total))
	return m, nil
}
