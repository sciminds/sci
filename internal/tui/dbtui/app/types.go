package app

// types.go — Type aliases re-exporting [tabstate] types for use
// within the app package. This avoids qualifying every reference with the
// tabstate package prefix (e.g. tabstate.Cell becomes just cell).
//
// Go type aliases (type X = Y) are transparent — values of the alias and the
// original type are interchangeable with no conversion. This pattern is common
// in Go when one package defines core types and another uses them heavily.
//
// Overlay state types (tableListState, cellEditorState, etc.) are defined
// directly in overlay_state.go rather than aliased, because they contain
// Bubble Tea widget imports that don't belong in the tabstate package.

import (
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
)

// Mode aliases.
type Mode = tabstate.Mode

const (
	modeNormal = tabstate.ModeNormal
	modeEdit   = tabstate.ModeEdit
	modeVisual = tabstate.ModeVisual
)

// Core data types.
type rowMeta = tabstate.RowMeta

const (
	sortAsc  = tabstate.SortAsc
	sortDesc = tabstate.SortDesc
)

type sortEntry = tabstate.SortEntry
type filterPin = tabstate.FilterPin
type Tab = tabstate.Tab
type cell = tabstate.Cell
type cellKind = tabstate.CellKind

const (
	cellText     = tabstate.CellText
	cellInteger  = tabstate.CellInteger
	cellReal     = tabstate.CellReal
	cellReadonly = tabstate.CellReadonly
)

type columnSpec = tabstate.ColumnSpec
type alignKind = tabstate.AlignKind

const (
	alignLeft  = tabstate.AlignLeft
	alignRight = tabstate.AlignRight
)

const (
	statusInfo  = tabstate.StatusInfo
	statusError = tabstate.StatusError
)

type statusMsg = tabstate.StatusMsg
type visualState = tabstate.VisualState
type statusHint = tabstate.StatusHint

const nullPinKey = tabstate.NullPinKey
