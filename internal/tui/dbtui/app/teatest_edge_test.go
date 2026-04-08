package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestReadOnlyEditBlocked verifies edit mode is blocked on forceRO model.
func TestTeatestReadOnlyEditBlocked(t *testing.T) {
	m := newReadOnlyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)

	sendKey(tm, "i")

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Error("edit mode should be blocked on read-only model")
	}
}

// TestTeatestEmptyDatabase verifies the TUI handles an empty database.
func TestTeatestEmptyDatabase(t *testing.T) {
	m := newEmptyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))

	// Send various keys — none should panic.
	sendKey(tm, "j")
	sendKey(tm, "k")
	sendKey(tm, "l")
	sendKey(tm, "/")
	sendKey(tm, "v")
	sendKey(tm, "i")

	// 't' should still open the (empty) table list.
	sendKey(tm, "t")

	fm := finalModel(t, tm)

	if fm.tableList == nil {
		t.Error("table list should open even on empty database")
	}
	if len(fm.tabs) != 0 {
		t.Errorf("expected 0 tabs, got %d", len(fm.tabs))
	}
}

// TestTeatestTerminalTooSmall verifies the too-small terminal message.
func TestTeatestTerminalTooSmall(t *testing.T) {
	m := newTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(10, 5))

	// The view should contain a too-small indicator (truncated at 10 cols).
	waitForOutput(t, tm, "Termi")

	fm := finalModel(t, tm)
	_ = fm
}

// TestTeatestWindowResize verifies the model handles resize messages.
func TestTeatestWindowResize(t *testing.T) {
	tm, _ := startTeatest(t)

	// Send a resize message.
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 40})

	fm := finalModel(t, tm)

	if fm.width != 120 {
		t.Errorf("width = %d, want 120", fm.width)
	}
	if fm.height != 40 {
		t.Errorf("height = %d, want 40", fm.height)
	}
}

// TestTeatestHelpOverlayClose verifies ? opens and Esc closes the help overlay.
func TestTeatestHelpOverlayClose(t *testing.T) {
	tm, _ := startTeatest(t)

	sendKey(tm, "?")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.helpVisible {
		t.Error("help overlay should be closed after Esc")
	}
}
