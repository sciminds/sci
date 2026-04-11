package app

import (
	tea "charm.land/bubbletea/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
)

// Model is the single Bubble Tea model for the board TUI. See doc.go for
// the architecture overview.
type Model struct {
	// ── Backend ──────────────────────────────────────────
	store        *engine.Store
	initialBoard string // board to auto-open on start, or ""

	// ── Screen state ─────────────────────────────────────
	screen screen
	status statusLine

	// Picker
	boards       []string
	pickerCursor int

	// Grid / Detail
	current  engine.Board // folded state of the currently-open board
	cur      cursor
	lastSeen string // last event ID seen (for Poll)

	// ── Layout ───────────────────────────────────────────
	width  int
	height int
	styles *ui.Styles
}

// NewModel constructs the Model. If initialBoard is non-empty the TUI
// fast-paths into that board's grid view on first render.
func NewModel(store *engine.Store, initialBoard string) *Model {
	return &Model{
		store:        store,
		initialBoard: initialBoard,
		screen:       screenPicker,
		styles:       ui.TUI,
		cur:          cursor{col: 0, card: -1},
	}
}

// Init fires the initial commands: list boards (always) and, if
// initialBoard is set, kick off a load for it so the grid pops up as
// soon as it's ready.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{listBoardsCmd(m.store)}
	if m.initialBoard != "" {
		cmds = append(cmds, loadBoardCmd(m.store, m.initialBoard))
	}
	return tea.Batch(cmds...)
}

// ── Small helpers ───────────────────────────────────────────────────────

// cardsByColumn groups the current board's cards by column ID, preserving
// position order within each column.
func (m *Model) cardsByColumn() map[string][]engine.Card {
	out := make(map[string][]engine.Card, len(m.current.Columns))
	for _, col := range m.current.Columns {
		out[col.ID] = nil
	}
	for _, c := range m.current.Cards {
		out[c.Column] = append(out[c.Column], c)
	}
	// Positional sort within each column.
	for id, list := range out {
		sortCardsByPosition(list)
		out[id] = list
	}
	return out
}

// focusedCard returns the card pointed at by m.cur, or nil if none.
func (m *Model) focusedCard() *engine.Card {
	if m.cur.col < 0 || m.cur.col >= len(m.current.Columns) {
		return nil
	}
	colID := m.current.Columns[m.cur.col].ID
	cards := m.cardsByColumn()[colID]
	if m.cur.card < 0 || m.cur.card >= len(cards) {
		return nil
	}
	return &cards[m.cur.card]
}

func (m *Model) setStatusInfo(text string) {
	m.status = statusLine{text: text, kind: statusInfo}
}

func (m *Model) setStatusError(text string) {
	m.status = statusLine{text: text, kind: statusError}
}
