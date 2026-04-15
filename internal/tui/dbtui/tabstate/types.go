// Package tabstate defines the core data types and pure operations for
// database table state in the TUI.
//
// This package contains no Bubble Tea framework imports (no tea.Cmd, tea.Msg).
// It depends only on [bubbles/table] for the table widget model, and on
// standard library packages.
//
// # Data Flow
//
// Each table is represented by a [Tab] struct, which holds three layers
// of row data that form a pipeline:
//
//   - FullCellRows: all rows as loaded from the database (never filtered)
//   - PostPinCellRows: rows after pin/column filtering (set by [ApplyRowFilter])
//   - CellRows: rows after search filtering (the final display set)
//
// Sorting ([ApplySorts]) operates on CellRows and FullCellRows together,
// keeping them in sync. Filtering ([ApplyRowFilter]) reads from Full* and
// writes to CellRows. Search filtering (in the app package) reads from
// PostPin* and writes to CellRows.
package tabstate

import (
	"charm.land/bubbles/v2/table"
)

// Mode is the top-level TUI interaction state.
type Mode int

// Mode constants for TUI interaction states.
const (
	ModeNormal Mode = iota
	ModeEdit
	ModeVisual
)

// RowMeta holds per-row metadata used for database operations (update, delete).
type RowMeta struct {
	ID     uint  // insertion order index (for stable sort tiebreaker)
	RowID  int64 // SQLite rowid
	Dimmed bool  // true in pin preview mode for non-matching rows
}

// SortDir indicates ascending or descending sort order.
type SortDir int

// SortDir constants for ascending and descending sort order.
const (
	SortAsc SortDir = iota
	SortDesc
)

// SortEntry represents one column in the multi-column sort stack.
type SortEntry struct {
	Col int
	Dir SortDir
}

// FilterPin holds the set of pinned values for a single column.
// Multiple values in the same column use OR (IN) semantics.
type FilterPin struct {
	Col    int             // index in tab.Specs
	Values map[string]bool // lowercased pinned values
}

// NullPinKey is the internal key for NULL cells in the pin system.
const NullPinKey = "\x00null"

// Tab represents a single database table displayed as a tab.
type Tab struct {
	Name       string
	Table      table.Model
	Rows       []RowMeta
	Specs      []ColumnSpec
	CellRows   [][]Cell
	ColCursor  int
	ViewOffset int // first visible column in horizontal scroll viewport
	ReadOnly   bool
	Loaded     bool // false = stub tab (name only); true = data loaded
	Loading    bool // true while async load is in-flight
	Sorts      []SortEntry

	// Pin-and-filter state.
	Pins           []FilterPin
	FilterActive   bool
	FilterInverted bool

	// Full data (pre-row-filter). These are the "source of truth" rows.
	FullRows     []table.Row
	FullMeta     []RowMeta
	FullCellRows [][]Cell

	// Snapshot of data after pin filtering (before search).
	// Set by [ApplyRowFilter]; consumed by the search filter in the app package.
	PostPinRows     []table.Row
	PostPinMeta     []RowMeta
	PostPinCellRows [][]Cell

	// CachedVP holds the last computed table viewport (nil = needs recompute).
	CachedVP interface{}
}

// InvalidateVP clears the cached viewport so it is recomputed on next render.
func (t *Tab) InvalidateVP() { t.CachedVP = nil }

// AlignKind controls horizontal text alignment within a column.
type AlignKind int

// AlignKind constants for horizontal text alignment.
const (
	AlignLeft AlignKind = iota
	AlignRight
)

// CellKind categorizes a cell for sorting and edit behavior.
type CellKind int

// CellKind constants categorizing cells for sorting and edit behavior.
const (
	CellText     CellKind = iota
	CellInteger           // INTEGER columns
	CellReal              // REAL/FLOAT columns
	CellReadonly          // computed/rowid columns (not editable)
)

// Cell is a single value in a table row.
type Cell struct {
	Value string
	Kind  CellKind
	Null  bool // true when the database value is NULL

	// SortKey, when non-empty, overrides Value for comparison in CompareCells.
	// Used for columns whose display format is not lexicographically ordered
	// (e.g. human dates like "04/11/25, 4:31pm"). Populated via
	// [data.SortKeyProvider]; ignored by numeric CellKind paths.
	SortKey string
}

// ColumnSpec describes a column's display properties and database metadata.
type ColumnSpec struct {
	Title     string    // display name
	DBName    string    // original column name from PRAGMA table_info (for SQL)
	Min       int       // minimum display width
	Max       int       // maximum display width (0 = uncapped)
	Flex      bool      // true = column grows to fill available space
	Align     AlignKind // horizontal alignment
	Kind      CellKind  // data type category
	HideOrder int       // 0 = visible; >0 = hidden (higher = more recently hidden)
	Expanded  bool      // when true, column uses full natural width
}

// StatusKind distinguishes informational from error status messages.
type StatusKind int

// StatusKind constants for status bar message types.
const (
	StatusInfo StatusKind = iota
	StatusError
)

// StatusMsg is a transient message displayed in the status bar.
type StatusMsg struct {
	Text string
	Kind StatusKind
}

// VisualState holds selection state for visual (multi-row select) mode.
type VisualState struct {
	Anchor   int          // row index where shift-selection started (-1 = none)
	Selected map[int]bool // individually toggled row indices (space key)

	// Cached selection set for rendering — invalidated when selection changes.
	CachedSet    map[int]bool
	CachedCursor int // cursor at time of cache computation
}

// StatusHint describes a status bar hint with progressive compaction.
type StatusHint struct {
	ID       string
	Full     string
	Compact  string
	Priority int
	Required bool
}

// Note: Overlay state types (TableListState, CellEditorState, ColumnRenameState,
// etc.) formerly lived here but were moved to app/overlay_state.go
// because they import Bubble Tea widgets (textarea, textinput) that don't
// belong in this framework-free package.
