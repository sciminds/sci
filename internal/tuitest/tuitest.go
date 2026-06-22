// Package tuitest provides shared helpers for teatest-based TUI tests.
//
// Each TUI package historically defined its own copies of SendKey,
// SendSpecial, WaitFor, and Final — see dbtui's app/TESTING.md for the
// established protocol. This package extracts the common subset so
// learn/help/labtui share one implementation; per-package term-size and
// timeout constants stay local since they vary by package.
package tuitest

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// SendKey sends a key press. Single-rune strings emit one KeyPressMsg
// with both Code and Text (a single keystroke like "j" or "/"). Multi-rune
// strings type each rune via tm.Type for natural text input.
func SendKey(tm *teatest.TestModel, key string) {
	runes := []rune(key)
	if len(runes) == 1 {
		tm.Send(tea.KeyPressMsg{Code: runes[0], Text: key})
		return
	}
	tm.Type(key)
}

// SendSpecial sends a special key (Enter, Escape, …) by Code only.
func SendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

// WaitFor blocks until tm.Output() contains substr, polling at high
// frequency so tests don't pay 50ms+ per assertion. Fails the test if
// wait elapses without a match.
//
// Output is matched after stripping ANSI via [ansi.Strip]. Alt-screen
// models repaint by cursor-diffing (and teatest wraps the program in
// tea.WithANSICompressor), so a target word can be split by escape
// sequences across frames — e.g. "python\x1b[4h-tutorials/". A raw
// bytes.Contains then misses a word that's plainly on screen, which under
// -race (different goroutine timing) turns into a flaky WaitFor timeout.
// Stripping first recovers the visible text and makes the match
// timing-independent. Prefer level-unique tokens (a breadcrumb path like
// "ejolly / python-tutorials") over list items that flicker through
// transient frames.
func WaitFor(t *testing.T, tm *teatest.TestModel, substr string, wait time.Duration) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(ansi.Strip(string(out)), substr)
	}, teatest.WithDuration(wait), teatest.WithCheckInterval(time.Millisecond))
}

// Final sends ctrl+c and returns the program's final model coerced to M.
// FinalModel blocks until the program exits and its Update goroutine has
// drained, so reads from the returned model are race-free.
func Final[M any](t *testing.T, tm *teatest.TestModel, timeout time.Duration) M {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(timeout)).(M)
}
