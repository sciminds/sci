package learn

import (
	"bytes"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// ── Integration: open/close/resize split view through the full model ──────

func TestGuideSplitViewResize(t *testing.T) {
	books := []Book{
		{
			Name:    "test",
			Heading: "Test Guide",
			Desc:    "Test book with split entry",
			Entries: []Entry{
				{
					Name:     "split-test",
					Cmd:      "split test — dual view",
					Desc:     "Entry with both cast and page",
					Category: "Test",
					CastFile: "ls.cast",
					PageFile: "python-basics.md",
				},
			},
		},
	}

	m := newModel(books)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Test Guide"))
	}, teatest.WithDuration(testWait))

	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")

	for _, size := range []tea.WindowSizeMsg{
		{Width: 200, Height: 50},
		{Width: 80, Height: 24},
		{Width: 50, Height: 15},
		{Width: 120, Height: 40},
	} {
		tm.Send(size)
	}

	fm := tFinalModel(t, tm)
	if fm.level != levelSplit {
		t.Errorf("should still be at split level after resizes, got %d", fm.level)
	}
	if fm.split == nil {
		t.Fatal("split should be non-nil after resizes")
	}
}

func TestGuideOpenSplitView(t *testing.T) {
	books := []Book{
		{
			Name:    "test",
			Heading: "Test Guide",
			Desc:    "Test book with split entry",
			Entries: []Entry{
				{
					Name:     "split-test",
					Cmd:      "split test — dual view",
					Desc:     "Entry with both cast and page",
					Category: "Test",
					CastFile: "ls.cast",
					PageFile: "python-basics.md",
				},
			},
		},
	}

	m := newModel(books)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Test Guide"))
	}, teatest.WithDuration(testWait))

	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")

	fm := tFinalModel(t, tm)
	if fm.level != levelSplit {
		t.Errorf("should be at split level, got %d", fm.level)
	}
	if fm.split == nil {
		t.Fatal("split should be non-nil after opening a dual entry")
	}
	if fm.player != nil {
		t.Error("standalone player should be nil in split mode")
	}
	if fm.viewer != nil {
		t.Error("standalone viewer should be nil in split mode")
	}
}

func TestGuideCloseSplitView(t *testing.T) {
	books := []Book{
		{
			Name:    "test",
			Heading: "Test Guide",
			Desc:    "Test book with split entry",
			Entries: []Entry{
				{
					Name:     "split-test",
					Cmd:      "split test — dual view",
					Desc:     "Entry with both cast and page",
					Category: "Test",
					CastFile: "ls.cast",
					PageFile: "python-basics.md",
				},
			},
		},
	}

	m := newModel(books)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testTermW, testTermH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Test Guide"))
	}, teatest.WithDuration(testWait))

	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")
	tSendSpecial(tm, tea.KeyEscape)

	fm := tFinalModel(t, tm)
	if fm.level != levelEntries {
		t.Errorf("should be back at entries level, got %d", fm.level)
	}
	if fm.split != nil {
		t.Error("split should be nil after closing")
	}
}
