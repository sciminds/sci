package guide

import (
	"bytes"
	"os"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

func skipUnlessSlow(t *testing.T) {
	t.Helper()
	if os.Getenv("SLOW") == "" {
		t.Skip("skipping teatest (set SLOW=1 to run)")
	}
}

const (
	testTermW = 100
	testTermH = 30
	testWait  = 2 * time.Second
	testFinal = 3 * time.Second
)

// ── Shared helpers ──────────────────────────────────────────────────────────

func startGuideTeatest(t *testing.T) *teatest.TestModel {
	t.Helper()
	skipUnlessSlow(t)
	m := newModel()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("ls"))
	}, teatest.WithDuration(testWait))
	return tm
}

func tSendKey(tm *teatest.TestModel, key string) {
	tm.Type(key)
}

func tSendSpecial(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code})
}

func tWaitForOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(substr))
	}, teatest.WithDuration(testWait))
}

func tFinalModel(t *testing.T, tm *teatest.TestModel) *model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(testFinal)).(*model)
}

// ── Integration tests ───────────────────────────────────────────────────────

func TestGuideRender(t *testing.T) {
	tm := startGuideTeatest(t)

	fm := tFinalModel(t, tm)
	if fm.player != nil {
		t.Error("player should be nil on initial render")
	}
	// List should have all entries
	if len(fm.list.Items()) != len(Entries) {
		t.Errorf("list has %d items, want %d", len(fm.list.Items()), len(Entries))
	}
}

func TestGuideOpenOverlay(t *testing.T) {
	tm := startGuideTeatest(t)

	// Press enter to open overlay on the first item
	tSendSpecial(tm, tea.KeyEnter)

	// Wait for the overlay (player) to render.
	tWaitForOutput(t, tm, "list files")

	fm := tFinalModel(t, tm)
	if fm.player == nil {
		t.Error("player should be non-nil after pressing enter")
	}
}

func TestGuideCloseOverlay(t *testing.T) {
	tm := startGuideTeatest(t)

	// Open overlay and wait for it to render.
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "list files")

	// Close overlay
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.player != nil {
		t.Error("player should be nil after closing overlay")
	}
}

func TestGuideQuit(t *testing.T) {
	tm := startGuideTeatest(t)

	tSendKey(tm, "q")

	fm := tFinalModel(t, tm)
	if !fm.quitting {
		t.Error("should be quitting after pressing q")
	}
}

func TestGuideFilteringBlocksQuit(t *testing.T) {
	tm := startGuideTeatest(t)

	// Enter filter mode (bubbles/list uses "/" to start filtering)
	tSendKey(tm, "/")
	// Wait for filter prompt to appear.
	tWaitForOutput(t, tm, "Filter")

	// Type 'q' — should go to filter input, not quit
	tSendKey(tm, "q")
	tWaitForOutput(t, tm, "q")

	fm := tFinalModel(t, tm)
	// The program should still be alive (ctrl+c from tFinalModel kills it).
	// The key check: list should be in filtering state, meaning "q" was
	// treated as filter input rather than a quit command.
	if fm.list.FilterState() != list.Filtering {
		t.Errorf("should be in filtering state, got %v", fm.list.FilterState())
	}
}
