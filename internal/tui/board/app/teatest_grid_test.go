package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// gridLoaded opens the first board from the picker and returns the test
// model on the grid screen, ready for grid-level key tests.
func gridLoaded(t *testing.T) *Model {
	t.Helper()
	tm := startTeatest(t)
	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")
	return finalModel(t, tm)
}

// TestGridInitialCursor: after loading, cursor sits at col=0 card=-1.
// (No card highlighted until the user presses j.)
func TestGridInitialCursor(t *testing.T) {
	fm := gridLoaded(t)
	if fm.cur.Col != 0 {
		t.Errorf("expected col=0, got %d", fm.cur.Col)
	}
	if fm.cur.Row != -1 {
		t.Errorf("expected card=-1, got %d", fm.cur.Row)
	}
}

// TestGridNavigateJ: j moves card cursor down within the current column.
func TestGridNavigateJ(t *testing.T) {
	tm := startTeatest(t)
	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")
	sendKey(tm, "j") // card 0
	sendKey(tm, "j") // card 1
	fm := finalModel(t, tm)
	if fm.cur.Col != 0 || fm.cur.Row != 1 {
		t.Errorf("expected col=0 card=1, got col=%d card=%d", fm.cur.Col, fm.cur.Row)
	}
}

// TestGridNavigateL: l moves column cursor right.
func TestGridNavigateL(t *testing.T) {
	tm := startTeatest(t)
	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")
	sendKey(tm, "l")
	fm := finalModel(t, tm)
	if fm.cur.Col != 1 {
		t.Errorf("expected col=1 after l, got %d", fm.cur.Col)
	}
}

// TestGridEnterOpensDetail: pressing enter with a card focused opens the
// detail screen for that card.
func TestGridEnterOpensDetail(t *testing.T) {
	tm := startTeatest(t)
	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")
	sendKey(tm, "j")              // focus first card
	sendSpecial(tm, tea.KeyEnter) // open detail
	waitForOutput(t, tm, "labels")
	fm := finalModel(t, tm)
	if fm.screen != screenDetail {
		t.Fatalf("expected screenDetail, got %v", fm.screen)
	}
}

// TestGridEscBacksToPicker: esc from grid returns to picker.
func TestGridEscBacksToPicker(t *testing.T) {
	tm := startTeatest(t)
	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")
	sendSpecial(tm, tea.KeyEscape)
	fm := finalModel(t, tm)
	if fm.screen != screenPicker {
		t.Errorf("expected screenPicker after esc, got %v", fm.screen)
	}
}
