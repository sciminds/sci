package app

// overlay_state.go — State types for overlay (modal) UI components.
//
// These types were moved from [tabstate] because they contain
// Bubble Tea widget imports (textarea, textinput) that don't belong in the
// framework-free tabstate package. They are only used within the app package.

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// columnPickerState holds the cursor for the hidden-column picker overlay.
type columnPickerState struct {
	Cursor int // index into hidden column indices
}

// notePreviewState holds the text shown in the note preview overlay.
type notePreviewState struct {
	Text    string
	Title   string
	Overlay ui.Overlay
}

// tableListState holds the state for the table list overlay.
type tableListState struct {
	Tables  []tableListEntry
	Cursor  int
	Status  string // transient status message shown in the overlay
	Adding  bool
	Browser *fileBrowserState

	Renaming    bool
	RenameInput textinput.Model

	Creating bool
	CreateEd *ui.LineEditor

	Deriving    bool
	DeriveName  textinput.Model // table/view name
	DeriveSQL   textarea.Model  // SQL query editor
	DeriveFocus int             // 0 = SQL editor, 1 = name editor
}

// fileBrowserState holds the state for the inline file browser.
type fileBrowserState struct {
	Dir     string
	Entries []fileBrowserEntry
	Cursor  int
}

// fileBrowserEntry is a single item in the file browser.
type fileBrowserEntry struct {
	Name  string
	IsDir bool
	Size  int64
}

// tableListEntry holds metadata for one table in the table list overlay.
type tableListEntry struct {
	Name      string
	Rows      int
	Columns   int
	IsView    bool
	IsVirtual bool
}

// columnRenameState holds the state for the column rename overlay.
type columnRenameState struct {
	Input     textinput.Model // text input for new name
	OldName   string          // current column name
	TableName string          // table the column belongs to
	ColIdx    int             // column index in tab.Specs
}

// cellEditorState holds the state for the inline cell editor overlay.
type cellEditorState struct {
	Editor    textarea.Model // multi-line text editor
	Title     string         // column name shown in header
	Original  string         // original cell value (for dirty detection)
	RowID     int64          // SQLite rowid
	ColName   string         // column name for the UPDATE
	TableName string         // table name for the UPDATE
	TabIdx    int            // index into Model.tabs
	RowIdx    int            // row index within tab.CellRows
	ColIdx    int            // column index within tab.Specs
}
