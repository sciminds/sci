package app

import (
	tea "charm.land/bubbletea/v2"
)

// handleKey routes a single key press based on the active screen.
// Global keys (quit, help) are handled before the per-screen switch.
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

	switch m.screen {
	case screenPicker:
		return m.handlePickerKey(msg)
	case screenGrid:
		return m.handleGridKey(msg)
	case screenDetail:
		return m.handleDetailKey(msg)
	}
	return m, nil
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
	curCards := byCol[cols[m.cur.col].ID]

	switch msg.String() {
	case "h", "left":
		if m.cur.col > 0 {
			m.cur.col--
			// Clamp card index to new column's length.
			n := len(byCol[cols[m.cur.col].ID])
			if m.cur.card >= n {
				m.cur.card = n - 1
			}
			m.ensureCursorVisible(m.width)
		}
	case "l", "right":
		if m.cur.col < len(cols)-1 {
			m.cur.col++
			n := len(byCol[cols[m.cur.col].ID])
			if m.cur.card >= n {
				m.cur.card = n - 1
			}
			m.ensureCursorVisible(m.width)
		}
	case "j", "down":
		if n := len(curCards); n > 0 {
			m.cur.card = (m.cur.card + 1) % n
		}
	case "k", "up":
		if n := len(curCards); n > 0 {
			// cur.card may be -1 (unfocused) — normalize before wrapping.
			prev := m.cur.card - 1
			if m.cur.card <= 0 {
				prev = n - 1
			}
			m.cur.card = prev
		}
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
