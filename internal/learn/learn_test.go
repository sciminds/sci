package learn

import (
	"bytes"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	testTermW = 100
	testTermH = 30
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

// ── Shared helpers ──────────────────────────────────────────────────────────

func startGuideTeatest(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := newModel(Books)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Terminal Guide"))
	}, teatest.WithDuration(testWait))
	return tm
}

// enterBook navigates into the first book (Terminal Guide) and waits for its entries.
func enterBook(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "ls")
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

func TestGuideRenderBooks(t *testing.T) {
	tm := startGuideTeatest(t)

	fm := tFinalModel(t, tm)
	if fm.level != levelBooks {
		t.Errorf("should be at book level, got %d", fm.level)
	}
	if len(fm.books.Items()) != len(Books) {
		t.Errorf("books list has %d items, want %d", len(fm.books.Items()), len(Books))
	}
}

func TestGuideEnterBook(t *testing.T) {
	tm := startGuideTeatest(t)
	enterBook(t, tm)

	fm := tFinalModel(t, tm)
	if fm.level != levelEntries {
		t.Errorf("should be at entries level, got %d", fm.level)
	}
	if len(fm.entries.Items()) != len(BasicEntries) {
		t.Errorf("entries list has %d items, want %d", len(fm.entries.Items()), len(BasicEntries))
	}
}

func TestGuideBackFromEntries(t *testing.T) {
	tm := startGuideTeatest(t)
	enterBook(t, tm)

	// Press esc to go back to books
	tSendSpecial(tm, tea.KeyEscape)
	tWaitForOutput(t, tm, "Guides")

	fm := tFinalModel(t, tm)
	if fm.level != levelBooks {
		t.Errorf("should be back at book level, got %d", fm.level)
	}
}

// enterGitBook navigates to the Git Guide (second book) and waits for entries.
func enterGitBook(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tSendSpecial(tm, tea.KeyDown)
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "git init")
}

func TestGuideOpenOverlay(t *testing.T) {
	tm := startGuideTeatest(t)
	// Use git learn — all its entries are cast-only.
	enterGitBook(t, tm)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "initialize")

	fm := tFinalModel(t, tm)
	if fm.level != levelOverlay {
		t.Errorf("should be at overlay level, got %d", fm.level)
	}
	if fm.player == nil {
		t.Error("player should be non-nil after pressing enter")
	}
}

func TestGuideCloseOverlay(t *testing.T) {
	tm := startGuideTeatest(t)
	enterGitBook(t, tm)

	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "initialize")

	// Close overlay
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.level != levelEntries {
		t.Errorf("should be back at entries level, got %d", fm.level)
	}
	if fm.player != nil {
		t.Error("player should be nil after closing overlay")
	}
}

func TestGuideQuitFromBooks(t *testing.T) {
	tm := startGuideTeatest(t)

	tSendKey(tm, "q")

	fm := tFinalModel(t, tm)
	if !fm.quitting {
		t.Error("should be quitting after pressing q at book level")
	}
}

func TestGuideQuitFromEntries(t *testing.T) {
	tm := startGuideTeatest(t)
	enterBook(t, tm)

	// q from entries goes back to books, not quit
	tSendKey(tm, "q")

	fm := tFinalModel(t, tm)
	if fm.level != levelBooks {
		t.Errorf("q from entries should go back to books, got level %d", fm.level)
	}
}

func TestGuideFilteringBlocksQuit(t *testing.T) {
	tm := startGuideTeatest(t)

	// Enter filter mode
	tSendKey(tm, "/")
	tWaitForOutput(t, tm, "Filter")

	// Type 'q' — should go to filter input, not quit
	tSendKey(tm, "q")
	tWaitForOutput(t, tm, "q")

	fm := tFinalModel(t, tm)
	if fm.books.FilterState() != list.Filtering {
		t.Errorf("should be in filtering state, got %v", fm.books.FilterState())
	}
}

// enterPythonBook navigates to the Python Guide book and waits for entries.
func enterPythonBook(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	// Navigate down to the Python Guide (third book)
	tSendSpecial(tm, tea.KeyDown)
	tSendSpecial(tm, tea.KeyDown)
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "Python Basics")
}

func TestGuideOpenPageOverlay(t *testing.T) {
	tm := startGuideTeatest(t)
	enterPythonBook(t, tm)

	// Press enter to open page overlay on the first item
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "Python Basics")

	fm := tFinalModel(t, tm)
	if fm.level != levelOverlay {
		t.Errorf("should be at overlay level, got %d", fm.level)
	}
	if fm.viewer == nil {
		t.Error("viewer should be non-nil after opening a page entry")
	}
	if fm.player != nil {
		t.Error("player should be nil for a page entry")
	}
}

func TestGuideClosePageOverlay(t *testing.T) {
	tm := startGuideTeatest(t)
	enterPythonBook(t, tm)

	// Open page overlay
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "Python Basics")

	// Close overlay
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.level != levelEntries {
		t.Errorf("should be back at entries level, got %d", fm.level)
	}
	if fm.viewer != nil {
		t.Error("viewer should be nil after closing overlay")
	}
}

func TestGuidePythonBookEntries(t *testing.T) {
	tm := startGuideTeatest(t)
	enterPythonBook(t, tm)

	fm := tFinalModel(t, tm)
	if fm.level != levelEntries {
		t.Errorf("should be at entries level, got %d", fm.level)
	}
	if len(fm.entries.Items()) != len(PythonEntries) {
		t.Errorf("entries list has %d items, want %d", len(fm.entries.Items()), len(PythonEntries))
	}
}
