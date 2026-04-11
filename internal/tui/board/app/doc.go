// Package app is the bubbletea Model, views, and key handling for the
// board TUI.
//
// # Architecture
//
// One Model, three screens, selected via Model.screen:
//
//   - screenPicker  — list of boards from Store.ListBoards
//   - screenGrid    — column × card kanban grid for a single board
//   - screenDetail  — read-only card detail pane
//
// The split across files mirrors internal/tui/dbtui/app:
//
//	types.go        enums, msgs, cursor types
//	model.go        Model struct, NewModel, Init, layout math
//	update.go       Update dispatcher (windowSize, msgs, then keys)
//	keys.go         key → action routing
//	cmds.go         tea.Cmd wrappers around board.Store
//	view.go         top-level View (chrome + body by screen)
//	view_picker.go  board picker body
//	view_grid.go    kanban grid body
//	view_detail.go  card detail body
//
// Styles all live under internal/tui/board/ui — never inline in view code.
package app
