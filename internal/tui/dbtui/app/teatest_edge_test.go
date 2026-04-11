package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestTeatestReadOnlyEditBlocked verifies edit mode is blocked on forceRO model.
func TestTeatestReadOnlyEditBlocked(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := newTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(10, 5))

	// The view should contain a too-small indicator (truncated at 10 cols).
	waitForOutput(t, tm, "Termi")

	fm := finalModel(t, tm)
	_ = fm
}

// TestTeatestWindowResize verifies the model handles resize messages.
func TestTeatestWindowResize(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "?")
	sendSpecial(tm, tea.KeyEscape)

	fm := finalModel(t, tm)

	if fm.helpVisible {
		t.Error("help overlay should be closed after Esc")
	}
}

// ── q key suppression in non-normal modes ──────────────────────────────

// TestTeatestQuitSuppressedInEditMode verifies q does NOT quit when in edit mode.
func TestTeatestQuitSuppressedInEditMode(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "i") // enter edit mode
	sendKey(tm, "q") // should NOT quit

	fm := finalModel(t, tm)

	if fm.mode != modeEdit {
		t.Errorf("mode = %d, want modeEdit (%d); q should not exit edit mode", fm.mode, modeEdit)
	}
}

// TestTeatestQuitSuppressedInVisualMode verifies q does NOT quit when in visual mode.
func TestTeatestQuitSuppressedInVisualMode(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "v") // enter visual mode
	sendKey(tm, "q") // should NOT quit

	fm := finalModel(t, tm)

	if fm.mode != modeVisual {
		t.Errorf("mode = %d, want modeVisual (%d); q should not exit visual mode", fm.mode, modeVisual)
	}
}

// TestTeatestQuitSuppressedWithOverlay verifies q does NOT quit when an overlay is open.
func TestTeatestQuitSuppressedWithOverlay(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "?") // open help overlay — dispatchOverlayKey consumes q
	sendKey(tm, "q") // should close help, not quit

	fm := finalModel(t, tm)

	// q with help open should close help (dispatchOverlayKey catches any key).
	// Model should still be alive (we can get finalModel).
	if fm.helpVisible {
		t.Error("help should be closed after q, not still visible")
	}
}

// ── n key exits edit and visual modes ──────────────────────────────────

// TestTeatestNKeyExitsEditMode verifies n exits edit mode.
func TestTeatestNKeyExitsEditMode(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "i") // enter edit
	sendKey(tm, "n") // exit via n

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after n in edit mode", fm.mode)
	}
}

// TestTeatestNKeyExitsVisualMode verifies n exits visual mode.
func TestTeatestNKeyExitsVisualMode(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "v") // enter visual
	sendKey(tm, "n") // exit via n

	fm := finalModel(t, tm)

	if fm.mode != modeNormal {
		t.Errorf("mode = %d, want modeNormal after n in visual mode", fm.mode)
	}
}

// ── Column rename/drop blocked on read-only ────────────────────────────

// TestTeatestColumnRenameBlockedReadOnly verifies r is blocked on read-only.
func TestTeatestColumnRenameBlockedReadOnly(t *testing.T) {
	t.Parallel()
	m := newReadOnlyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)

	sendKey(tm, "r") // attempt column rename

	fm := finalModel(t, tm)

	if fm.columnRename != nil {
		t.Error("column rename overlay should NOT open on read-only table")
	}
}

// TestTeatestColumnDropBlockedReadOnly verifies D does not drop columns on read-only tables.
func TestTeatestColumnDropBlockedReadOnlyFull(t *testing.T) {
	t.Parallel()
	m := newReadOnlyTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	waitForTable(t, tm)

	sendKey(tm, "D") // attempt column drop

	fm := finalModel(t, tm)

	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("no active tab")
	}
	// Products has 3 columns (id, title, price). All should still be present.
	if len(tab.Specs) != 3 {
		t.Errorf("expected 3 columns (drop should be blocked), got %d", len(tab.Specs))
	}
}

// ── Window resize during overlays ──────────────────────────────────────

// TestTeatestResizeDuringNotePreview verifies resize updates dimensions with preview open.
func TestTeatestResizeDuringNotePreview(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	// Open note preview.
	sendSpecial(tm, tea.KeyEnter)
	// Resize while preview is open.
	tm.Send(tea.WindowSizeMsg{Width: 150, Height: 50})

	fm := finalModel(t, tm)

	if fm.width != 150 || fm.height != 50 {
		t.Errorf("dimensions = %dx%d, want 150x50", fm.width, fm.height)
	}
	if fm.notePreview == nil {
		t.Error("note preview should still be open after resize")
	}
}

// TestTeatestResizeDuringCellEditor verifies resize updates cell editor dimensions.
func TestTeatestResizeDuringCellEditor(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "i")              // enter edit mode
	sendSpecial(tm, tea.KeyEnter) // open cell editor
	tm.Send(tea.WindowSizeMsg{Width: 130, Height: 45})

	fm := finalModel(t, tm)

	if fm.width != 130 || fm.height != 45 {
		t.Errorf("dimensions = %dx%d, want 130x45", fm.width, fm.height)
	}
	if fm.cellEditor == nil {
		t.Error("cell editor should still be open after resize")
	}
}

// TestTeatestResizeDuringDeriveOverlay verifies resize updates derive textarea dimensions.
func TestTeatestResizeDuringDeriveOverlay(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	sendKey(tm, "t") // open table list
	sendKey(tm, "s") // start derive
	tm.Send(tea.WindowSizeMsg{Width: 140, Height: 50})

	fm := finalModel(t, tm)

	if fm.width != 140 || fm.height != 50 {
		t.Errorf("dimensions = %dx%d, want 140x50", fm.width, fm.height)
	}
	if fm.tableList == nil || !fm.tableList.Deriving {
		t.Error("derive overlay should still be open after resize")
	}
}

// TestTeatestResizeShrinkToTooSmall verifies shrinking below minimum is handled.
func TestTeatestResizeShrinkToTooSmall(t *testing.T) {
	t.Parallel()
	tm, _ := startTeatest(t)

	// Shrink well below minimums.
	tm.Send(tea.WindowSizeMsg{Width: 15, Height: 5})

	fm := finalModel(t, tm)

	if fm.width != 15 || fm.height != 5 {
		t.Errorf("dimensions = %dx%d, want 15x5", fm.width, fm.height)
	}
	// Model should still be functional — no panic.
}

// TestTeatestResizeGrowFromTooSmall verifies recovering from a too-small terminal.
func TestTeatestResizeGrowFromTooSmall(t *testing.T) {
	t.Parallel()
	m := newTeatestModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(10, 5))

	// Wait for the too-small state to commit before sending the grow.
	// Without this barrier, the grow message can race against teatest's
	// own initial WindowSizeMsg(10, 5) injected by WithInitialTermSize,
	// letting the stale message clobber width back to 10 after we grew.
	waitForOutput(t, tm, "Termi")

	// Start too small, then grow to usable size.
	tm.Send(tea.WindowSizeMsg{Width: 100, Height: 30})
	waitForTable(t, tm)

	fm := finalModel(t, tm)

	if fm.width != 100 || fm.height != 30 {
		t.Errorf("dimensions = %dx%d, want 100x30", fm.width, fm.height)
	}
	tab := fm.effectiveTab()
	if tab == nil {
		t.Fatal("should have a usable tab after growing")
	}
}
