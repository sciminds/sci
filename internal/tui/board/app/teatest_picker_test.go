package app

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestPickerRendersBoardList: on launch the picker shows each board ID
// from Store.ListBoards. With a single fixture board "alpha" we expect
// to see it in the rendered output.
func TestPickerRendersBoardList(t *testing.T) {
	tm := startTeatest(t)

	fm := finalModel(t, tm)
	if fm.screen != screenPicker {
		t.Fatalf("expected screenPicker, got %v", fm.screen)
	}
	if len(fm.boards) == 0 || fm.boards[0] != "alpha" {
		t.Fatalf("expected boards=[alpha], got %v", fm.boards)
	}
}

// TestPickerEnterLoadsBoard: pressing enter on the selected board
// triggers a Store.Load and transitions to the grid screen.
func TestPickerEnterLoadsBoard(t *testing.T) {
	tm := startTeatest(t)

	sendSpecial(tm, tea.KeyEnter)
	waitForOutput(t, tm, "Write tests")

	fm := finalModel(t, tm)
	if fm.screen != screenGrid {
		t.Fatalf("expected screenGrid after enter, got %v", fm.screen)
	}
	if fm.current.ID != "alpha" {
		t.Fatalf("expected loaded board alpha, got %q", fm.current.ID)
	}
}

// TestPickerQuitsFromPicker: q at the picker exits the program.
func TestPickerQuitsFromPicker(t *testing.T) {
	tm := startTeatest(t)

	sendKey(tm, "q")
	// q at picker should trigger tea.Quit. FinalOutput times out otherwise.
	_, err := io.ReadAll(tm.FinalOutput(t, teatest.WithFinalTimeout(testFinal)))
	if err != nil {
		t.Fatal(err)
	}
}
