package app

import (
	tea "charm.land/bubbletea/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/uikit"
)

// Update is the top-level message dispatcher. Window resizes and async
// store results are handled here; keys are delegated to keys.go.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case uikit.Result[[]string]: // listBoardsCmd result
		if msg.Err != nil {
			m.setStatusError("list boards: " + msg.Err.Error())
			return m, nil
		}
		m.boards = msg.Value
		if m.pickerCursor >= len(m.boards) {
			m.pickerCursor = 0
		}
		// Chain into the initial board load only once, after the picker
		// list is populated. Keeps Init() sequential so tab-cycling knows
		// which boards exist by the time the grid screen renders.
		if m.initialBoard != "" && m.current.ID == "" {
			board := m.initialBoard
			m.initialBoard = ""
			return m, loadBoardCmd(m.store, board)
		}
		return m, nil

	case uikit.Result[engine.Board]: // loadBoardCmd result
		if msg.Err != nil {
			m.setStatusError("load board: " + msg.Err.Error())
			return m, nil
		}
		m.current = msg.Value
		m.screen = screenGrid
		m.cur = uikit.Grid2D{Col: 0, Row: -1}
		m.gridScroll = 0
		m.collapsed = map[string]bool{}
		// One-shot: apply initialGridCol on the next board load (the
		// initial one, since the field is reset below).
		if m.initialGridCol > 0 {
			n := len(msg.Value.Columns)
			if m.initialGridCol < n {
				m.cur.Col = m.initialGridCol
				m.gridScroll = m.initialGridCol
				m.ensureCursorVisible(m.width)
			}
		}
		m.initialGridCol = -1
		m.setStatusInfo("loaded " + msg.Value.Title)
		return m, pollCmd(m.store, msg.Value.ID, m.lastSeen)

	case uikit.Result[struct{}]: // AppendCmd result
		if msg.Err != nil {
			m.setStatusError("sync: " + msg.Err.Error())
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
