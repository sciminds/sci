// Package app implements the TUI using the Bubble Tea Model-View-Update pattern.
//
// # Architecture
//
// The entire TUI is a single [Model] implementing [tea.Model]. This is standard
// Bubble Tea — one model owns all state, and the framework calls Init, Update,
// and View in a loop.
//
//   - [Model]  (model.go)  — state container, implements tea.Model
//   - Init     (model.go)  — returns the initial Cmd (spinner tick)
//   - Update   (update.go) — message dispatcher: window size, keys, mouse, async results
//   - View     (view.go)   — pure render: composes tab bar + table + status + overlays
//
// # File Organization
//
// Files are named by their role in the MVU pattern:
//
//	## Core MVU skeleton
//	model.go                — Model struct, NewModel, Init
//	update.go               — Update(), handleKey(), handleMouse(), updateEditors()
//	view.go                 — View(), buildView(), tab bar, table composition
//
//	## Update helpers (state mutation)
//	tab.go                  — Tab lifecycle: buildTab, switchToTab, triggerTabLoad
//	cursor.go               — Cursor navigation: up/down, left/right, page, selection
//
//	## Keys
//	keys.go                 — Key string constants + display symbols
//	keys_dispatch.go        — Normal-mode key handler
//	keymap_help.go          — Help overlay keymaps (bubbles/help display)
//
//	## View helpers (pure rendering)
//	view_table.go           — Table grid layout + cell rendering
//	view_status.go          — Status bar + mode hints
//	view_helpers.go         — Rendering utilities, keycap rendering, tabstate wrappers
//
//	## Feature modules (Update + View for one feature)
//	search.go               — Incremental search
//	visual.go               — Visual mode (select, yank, paste)
//	cell_editor.go          — Cell editor overlay
//	column_ops.go           — Column rename/drop
//	column_picker.go        — Column visibility picker
//
//	## Table list (split by sub-feature)
//	table_list.go           — Core: toggle, navigate, delete, export, dedup, rename
//	table_list_browse.go    — File browser
//	table_list_create.go    — Create/derive tables
//	table_list_render.go    — Overlay rendering
//
//	## Support
//	overlay_state.go        — Overlay state types (tableListState, cellEditorState, etc.)
//	types.go                — Type aliases from tabstate
//	run.go                  — Entry point (tea.NewProgram)
//
// # Key Dispatch Chain
//
// Keystrokes flow through a layered dispatch:
//
//	Update → handleKey → dispatchOverlayKey    (if an overlay is open)
//	                   → handleVisualKey       (if in visual mode)
//	                   → edit: Enter opens cell (inline in update.go)
//	                   → handleNormalModeKey    (keys_dispatch.go)
//
// Key handlers live in update.go (overlay + edit + shared nav) and
// keys_dispatch.go (normal mode). All key string constants are in keys.go.
//
// # Overlays
//
// Each overlay (help, cell editor, search bar, table list, column picker) is a
// pointer field on Model. A nil pointer means the overlay is closed; non-nil
// means it is open. At most one overlay is active at a time. Overlay state
// types are defined in overlay_state.go.
//
// # Async Tab Loading
//
// Tabs are initialized as stubs (name only, Loaded=false). When a tab is
// selected, [triggerTabLoad] spawns a Cmd that calls [buildTab] in the
// background, returning a [tabLoadedMsg] when done. This avoids blocking the
// UI on large tables.
//
// # Data Pipeline
//
// Row data flows through three layers (defined in [tabstate]):
//
//	FullCellRows  →  PostPinCellRows  →  CellRows
//	(all rows)       (after pin filter)   (after search filter)
//
// Sorting operates on all layers simultaneously. Pin filtering reads from
// Full* and writes to CellRows + PostPin*. Search filtering reads from
// PostPin* and writes to CellRows.
//
// # Package Dependencies
//
//	app
//	  ├── tabstate  (Tab types, sort, filter — no tea.Cmd/tea.Msg)
//	  ├── match     (fuzzy/substring matching — no UI deps)
//	  ├── data      (DataStore interface, SQLite backend)
//	  ├── ui        (lipgloss styles, layout constants)
//	  └── bubbletea + bubbles (framework)
package app
