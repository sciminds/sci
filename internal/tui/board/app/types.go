package app

// screen is the top-level view the Model is currently rendering.
type screen int

const (
	screenPicker screen = iota
	screenGrid
	screenDetail
)

// ── Messages ────────────────────────────────────────────────────────────

// pollMsg is emitted when Store.Poll returns new remote event IDs.
// Kept as a custom type because pollCmd uses tea.Tick, which doesn't
// fit kit.AsyncCmdCtx.
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
