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
	current        engine.Board // folded state of the currently-open board
	cur            cursor
	lastSeen       string          // last event ID seen (for Poll)
	gridScroll     int             // leftmost visible column in windowed mode
	collapsed      map[string]bool // column IDs currently rendered as collapsed strips
	initialGridCol int             // cursor column to apply when initialBoard first loads

	// ── Layout ───────────────────────────────────────────
	width  int
	height int
	styles *ui.Styles
}

// NewModel constructs the Model. If initialBoard is non-empty the TUI
// fast-paths into that board's grid view on first render.
func NewModel(store *engine.Store, initialBoard string) *Model {
	return &Model{
		store:          store,
		initialBoard:   initialBoard,
		screen:         screenPicker,
		styles:         ui.TUI,
		cur:            cursor{col: 0, card: -1},
		collapsed:      map[string]bool{},
		initialGridCol: -1,
	}
}

// SetInitialGridCursor places the grid cursor on the given column the
// first time the initialBoard loads. Used by callers that want to open a
// specific column in view (e.g. the calendar demo opening on the current
// month). No effect on subsequent board loads.
func (m *Model) SetInitialGridCursor(col int) {
	m.initialGridCol = col
}

// Init fires listBoards first; if initialBoard is set, the boardsLoaded
// handler chains a loadBoardCmd once the board list is in hand. That
// ordering guarantees m.boards is populated before the grid screen
// renders — which matters for tab cycling between boards.
func (m *Model) Init() tea.Cmd {
	return listBoardsCmd(m.store)
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
