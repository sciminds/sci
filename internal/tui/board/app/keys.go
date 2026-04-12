package app

import (
	tea "charm.land/bubbletea/v2"
)

// handleKey routes a single key press based on the active screen.
// Global keys (quit, help) are handled before the per-screen dispatch.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Global.
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q":
		switch m.screen {
		case screenPicker:
			return m, tea.Quit
		case screenDetail:
			m.screen = screenGrid
			return m, nil
		case screenGrid:
			m.screen = screenPicker
			return m, nil
		}
	}

	return m.router.Keys(m.screen, m, msg)
}

func (m *Model) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.pickerCursor < len(m.boards)-1 {
			m.pickerCursor++
		}
	case "k", "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
	case "enter", "l", "right":
		if len(m.boards) == 0 {
			return m, nil
		}
		return m, loadBoardCmd(m.store, m.boards[m.pickerCursor])
	case "r":
		return m, listBoardsCmd(m.store)
	}
	return m, nil
}

func (m *Model) handleGridKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cols := m.current.Columns
	if len(cols) == 0 {
		return m, nil
	}
	byCol := m.cardsByColumn()
	rowsIn := func(col int) int {
		if col < 0 || col >= len(cols) {
			return 0
		}
		return len(byCol[cols[col].ID])
	}

	switch msg.String() {
	case "h", "left":
		m.cur.Move(-1, 0, len(cols), rowsIn)
		m.ensureCursorVisible(m.width)
	case "l", "right":
		m.cur.Move(1, 0, len(cols), rowsIn)
		m.ensureCursorVisible(m.width)
	case "j", "down":
		m.cur.Move(0, 1, len(cols), rowsIn)
	case "k", "up":
		m.cur.Move(0, -1, len(cols), rowsIn)
	case "c":
		m.toggleCollapseCurrent()
		m.ensureCursorVisible(m.width)
	case "C":
		m.expandAll()
	case "tab":
		if next := m.siblingBoardID(+1); next != "" {
			return m, loadBoardCmd(m.store, next)
		}
	case "shift+tab":
		if prev := m.siblingBoardID(-1); prev != "" {
			return m, loadBoardCmd(m.store, prev)
		}
	case "enter":
		if m.focusedCard() != nil {
			m.screen = screenDetail
		}
	case "r":
		return m, loadBoardCmd(m.store, m.current.ID)
	case "esc":
		m.screen = screenPicker
	}
	return m, nil
}

// siblingBoardID returns the board ID offset from m.current.ID in
// m.boards (wrapping at both ends). Returns "" if there are fewer than
// two boards or the current board isn't in the list.
func (m *Model) siblingBoardID(delta int) string {
	n := len(m.boards)
	if n < 2 {
		return ""
	}
	idx := -1
	for i, id := range m.boards {
		if id == m.current.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}
	next := (idx + delta%n + n) % n
	return m.boards[next]
}

func (m *Model) handleDetailKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "h", "left":
		m.screen = screenGrid
	}
	return m, nil
}
