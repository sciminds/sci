package app

import (
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
	"github.com/sciminds/cli/internal/tui/kit"
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
	cur            kit.Grid2D
	lastSeen       string          // last event ID seen (for Poll)
	gridScroll     int             // leftmost visible column in windowed mode
	collapsed      map[string]bool // column IDs currently rendered as collapsed strips
	initialGridCol int             // cursor column to apply when initialBoard first loads

	// ── Routing ─────────────────────────────────────────
	router kit.Router[screen, *Model]

	// ── Layout ───────────────────────────────────────────
	width  int
	height int
	styles *ui.Styles
}

// NewModel constructs the Model. If initialBoard is non-empty the TUI
// fast-paths into that board's grid view on first render.
func NewModel(store *engine.Store, initialBoard string) *Model {
	m := &Model{
		store:          store,
		initialBoard:   initialBoard,
		screen:         screenPicker,
		styles:         ui.TUI,
		cur:            kit.Grid2D{Col: 0, Row: -1},
		collapsed:      map[string]bool{},
		initialGridCol: -1,
	}
	m.router = buildRouter()
	return m
}

// buildRouter wires each screen to its View / Keys / Title / Help.
// Called once at construction time.
func buildRouter() kit.Router[screen, *Model] {
	return kit.NewRouter(map[screen]kit.Screen[*Model]{
		screenPicker: {
			View:  (*Model).viewPicker,
			Keys:  (*Model).handlePickerKey,
			Title: func(_ *Model, _ int) string { return "sci board" },
			Help:  "j/k move  ↵ open  r reload  q quit",
		},
		screenGrid: {
			View: (*Model).viewGrid,
			Keys: (*Model).handleGridKey,
			Title: func(m *Model, _ int) string {
				return "sci board · " + m.current.Title
			},
			Help: "hjkl move  c collapse  C expand  tab switch board  ↵ detail  esc back  q quit",
		},
		screenDetail: {
			View: (*Model).viewDetail,
			Keys: (*Model).handleDetailKey,
			Title: func(m *Model, _ int) string {
				if c := m.focusedCard(); c != nil {
					return "sci board · " + m.current.Title + " · " + c.Title
				}
				return "sci board · " + m.current.Title
			},
			Help: "esc back  q grid",
		},
	})
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
	out := lo.SliceToMap(m.current.Columns, func(col engine.Column) (string, []engine.Card) {
		return col.ID, nil
	})
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
	if m.cur.Col < 0 || m.cur.Col >= len(m.current.Columns) {
		return nil
	}
	colID := m.current.Columns[m.cur.Col].ID
	cards := m.cardsByColumn()[colID]
	if m.cur.Row < 0 || m.cur.Row >= len(cards) {
		return nil
	}
	return &cards[m.cur.Row]
}

func (m *Model) setStatusInfo(text string) {
	m.status = statusLine{text: text, kind: statusInfo}
}

func (m *Model) setStatusError(text string) {
	m.status = statusLine{text: text, kind: statusError}
}
