package uikit

// color_symbols.go — Unicode icon constants, pre-rendered symbols, and
// convenience print helpers for CLI status output.

import (
	"fmt"
	"os"
)

// Icon constants — raw strings so callers can apply their own styles.
const (
	IconPass    = "✓"
	IconFail    = "✗"
	IconWarn    = "⚠"
	IconPending = "○"
	IconArrow   = "→"
	IconCursor  = "❯"
	IconDot     = "●"
	IconSkip    = "–"
)

// Pre-rendered symbols for non-TUI CLI output.
var (
	SymOK    = TUI.Pass().Render(IconPass)
	SymFail  = TUI.Fail().Render(IconFail)
	SymWarn  = TUI.Warn().Render(IconWarn)
	SymArrow = TUI.TextBlue().Render(IconArrow)
)

// ── Printf-style status helpers ────────────────────────────────────────────

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
	_, _ = fmt.Fprintf(statusOut(), "\n  %s %s\n", SymArrow, TUI.TextBlue().Render(cmd))
	Hint(desc)
}
