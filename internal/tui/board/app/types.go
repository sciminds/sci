package app

import (
	engine "github.com/sciminds/cli/internal/board"
)

// screen is the top-level view the Model is currently rendering.
type screen int

const (
	screenPicker screen = iota
	screenGrid
	screenDetail
)

// cursor tracks the focused column/card within the grid view.
// -1 for card means "column selected, no card highlighted".
type cursor struct {
	col  int
	card int
}

// ── Messages ────────────────────────────────────────────────────────────

// boardsLoadedMsg is emitted when Store.ListBoards completes.
type boardsLoadedMsg struct {
	ids []string
	err error
}

// boardLoadedMsg is emitted when Store.Load completes for a single board.
type boardLoadedMsg struct {
	board engine.Board
	err   error
}

// appendDoneMsg is emitted after an optimistic Store.Append completes.
// The event has already been durably queued so err is informational only —
// the UI does not need to roll back on failure.
type appendDoneMsg struct {
	err error
}

// pollMsg is emitted when Store.Poll returns new remote event IDs.
type pollMsg struct {
	newIDs []string
	err    error
}

// statusKind classifies transient status bar messages.
type statusKind int

const (
	statusInfo statusKind = iota
	statusError
)

type statusLine struct {
	text string
	kind statusKind
}
