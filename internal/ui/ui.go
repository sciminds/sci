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
//   - [RunWithSpinner] wraps a long-running function with a spinner
//   - [ClampWidth] / [ClampHeight] guard Bubble Tea views against zero dimensions
//
// Symbols are available as package variables: [SymOK] (✓), [SymFail] (✗),
// [SymWarn] (⚠), [SymArrow] (→).
package ui

import (
	"fmt"
	"os"
)

// OK prints a green check line.
func OK(msg string) { fmt.Printf("%s %s\n", SymOK, msg) }

// Fail prints a red X line to stderr.
func Fail(msg string) { fmt.Fprintf(os.Stderr, "%s %s\n", SymFail, msg) }

// Warn prints a yellow warning line.
func Warn(msg string) { fmt.Printf("%s %s\n", SymWarn, msg) }

// Hint prints a dimmed indented line.
func Hint(msg string) { fmt.Printf("  %s\n", TUI.Dim().Render(msg)) }

// Header prints a bold section header.
func Header(msg string) { fmt.Printf("\n  %s\n\n", TUI.Bold().Render(msg)) }

// NextStep prints a suggested next action after a command completes.
func NextStep(cmd, desc string) {
	fmt.Printf("\n  %s %s\n", SymArrow, TUI.Accent().Render(cmd))
	Hint(desc)
}
