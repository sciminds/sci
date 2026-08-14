package uikit

// ui_severity.go — the one severity vocabulary for status text across sci's
// TUIs: four levels, each with a label, an icon, and a palette style.
//
// It exists so a surface that renders its own status line does not re-derive a
// parallel switch over the same four palette calls. uikit/browser aliases its
// StatusKind to Severity for exactly that reason.

import (
	"charm.land/lipgloss/v2"
)

// Severity is how serious a piece of status text is.
type Severity int

// Severity levels, ordered least to most severe.
const (
	SeverityInfo    Severity = iota // informational
	SeveritySuccess                 // operation succeeded
	SeverityWarning                 // non-fatal warning
	SeverityError                   // error
)

// String returns a human-readable label for the level.
func (l Severity) String() string {
	switch l {
	case SeverityInfo:
		return "info"
	case SeveritySuccess:
		return "success"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// Icon returns the Unicode icon for the level (from color_symbols.go).
func (l Severity) Icon() string {
	switch l {
	case SeverityInfo:
		return IconArrow
	case SeveritySuccess:
		return IconPass
	case SeverityWarning:
		return IconWarn
	case SeverityError:
		return IconFail
	default:
		return IconDot
	}
}

// Style returns the lipgloss style for the level from the shared TUI singleton.
func (l Severity) Style() lipgloss.Style {
	switch l {
	case SeverityInfo:
		return TUI.TextBlue()
	case SeveritySuccess:
		return TUI.Pass()
	case SeverityWarning:
		return TUI.Warn()
	case SeverityError:
		return TUI.Fail()
	default:
		return TUI.Dim()
	}
}
