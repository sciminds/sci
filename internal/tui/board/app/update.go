package app

import (
	tea "charm.land/bubbletea/v2"
)

// Update is the top-level message dispatcher. Window resizes and async
// store results are handled here; keys are delegated to keys.go.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case boardsLoadedMsg:
		if msg.err != nil {
			m.setStatusError("list boards: " + msg.err.Error())
			return m, nil
		}
		m.boards = msg.ids
		if m.pickerCursor >= len(m.boards) {
			m.pickerCursor = 0
		}
		return m, nil

	case boardLoadedMsg:
		if msg.err != nil {
			m.setStatusError("load board: " + msg.err.Error())
			return m, nil
		}
		m.current = msg.board
		m.screen = screenGrid
		m.cur = cursor{col: 0, card: -1}
		m.setStatusInfo("loaded " + msg.board.Title)
		return m, pollCmd(m.store, msg.board.ID, m.lastSeen)

	case appendDoneMsg:
		if msg.err != nil {
			m.setStatusError("sync: " + msg.err.Error())
		}
		return m, nil

	case pollMsg:
		if msg.err == nil && len(msg.newIDs) > 0 {
			m.setStatusInfo("updates pending — press r to reload")
		}
		// Always re-schedule (even on error) so transient failures don't kill polling.
		if m.current.ID != "" {
			return m, pollCmd(m.store, m.current.ID, m.lastSeen)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}
