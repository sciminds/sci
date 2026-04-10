// Package ui provides centralized styling and output helpers for the terminal.
//
// All visual output in the CLI goes through this package. The [TUI] singleton
// exposes style methods (Dim, Bold, Accent, Pass, Fail, Warn) that return
// lipgloss styles — call .Render(text) to apply them.
//
// Convenience functions for common patterns:
//
//   - [OK] / [Fail] / [Warn] print status lines with colored symbols
//   - [Header] prints a bold section heading
//   - [NextStep] suggests a follow-up command after an operation
//   - [RunWithSpinner] wraps a long-running function with a bubbletea inline spinner
//   - [ClampWidth] / [ClampHeight] guard Bubble Tea views against zero dimensions
//
// Symbols are available as package variables: [SymOK] (✓), [SymFail] (✗),
// [SymWarn] (⚠), [SymArrow] (→).
package ui

import (
	"fmt"
	"os"
)

// statusOut returns stderr in quiet mode (so status output doesn't
// pollute JSON on stdout), stdout otherwise.
func statusOut() *os.File {
	if IsQuiet() {
		return os.Stderr
	}
	return os.Stdout
}

// OK prints a green check line.
func OK(msg string) { _, _ = fmt.Fprintf(statusOut(), "%s %s\n", SymOK, msg) }

// Hint prints a dimmed indented line.
func Hint(msg string) { _, _ = fmt.Fprintf(statusOut(), "  %s\n", TUI.Dim().Render(msg)) }

// Header prints a bold section header.
func Header(msg string) { _, _ = fmt.Fprintf(statusOut(), "\n  %s\n\n", TUI.Bold().Render(msg)) }

// NextStep prints a suggested next action after a command completes.
func NextStep(cmd, desc string) {
	_, _ = fmt.Fprintf(statusOut(), "\n  %s %s\n", SymArrow, TUI.Accent().Render(cmd))
	Hint(desc)
}
