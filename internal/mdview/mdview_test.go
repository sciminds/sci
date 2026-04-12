package mdview

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	testW    = 100
	testH    = 30
	testWait = 5 * time.Second
	testFin  = 8 * time.Second
)

var testPages = []Page{
	{Name: "alpha", Content: "# Alpha\n\nFirst page."},
	{Name: "beta", Content: "# Beta\n\nSecond page."},
}

func startSinglePage(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := New([]Page{{Name: "test", Content: "# Test\n\nHello world."}})
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testW, testH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Hello"))
	}, teatest.WithDuration(testWait))
	return tm
}

func startMultiPage(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := New(testPages)
	m.initPicker()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(testW, testH))
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("alpha"))
	}, teatest.WithDuration(testWait))
	return tm
}

func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	return tm.FinalModel(t, teatest.WithFinalTimeout(testFin)).(*Model)
}

func TestSinglePageRenders(t *testing.T) {
	t.Parallel()
	tm := startSinglePage(t)
	fm := finalModel(t, tm)
	if fm.level != levelViewer {
		t.Errorf("single page should start at viewer level, got %d", fm.level)
	}
	if fm.multi {
		t.Error("single page should not be in multi mode")
	}
}

func TestMultiPageShowsPicker(t *testing.T) {
	t.Parallel()
	tm := startMultiPage(t)
	fm := finalModel(t, tm)
	if fm.level != levelPicker {
		t.Errorf("multi page should start at picker level, got %d", fm.level)
	}
}

func TestMultiPageOpenAndClose(t *testing.T) {
	t.Parallel()
	tm := startMultiPage(t)

	// Open first page
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Alpha"))
	}, teatest.WithDuration(testWait))

	// Close with esc
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("alpha"))
	}, teatest.WithDuration(testWait))

	fm := finalModel(t, tm)
	if fm.level != levelPicker {
		t.Errorf("should be back at picker after esc, got %d", fm.level)
	}
}

func TestMultiPageSwitchRendersNewContent(t *testing.T) {
	t.Parallel()
	tm := startMultiPage(t)

	// Open first page (alpha)
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("First page"))
	}, teatest.WithDuration(testWait))

	// Go back to picker
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("beta"))
	}, teatest.WithDuration(testWait))

	// Move down to beta and open it
	tm.Send(tea.KeyPressMsg{Code: 'j'})
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("Second page"))
	}, teatest.WithDuration(testWait))

	fm := finalModel(t, tm)
	if fm.current != 1 {
		t.Errorf("should be viewing page 1 (beta), got %d", fm.current)
	}
	if !strings.Contains(fm.rendered, "Second") {
		t.Error("rendered content should be from beta page, but still shows alpha")
	}
}

func TestViewerScrollPercent(t *testing.T) {
	t.Parallel()
	v := NewViewer("test", "# Test\n\nLine 1\nLine 2\nLine 3")
	v.SetSize(80, 50) // tall enough to fit all content
	pct := v.ScrollPercent()
	if pct != 100 {
		t.Errorf("short content should be 100%%, got %d%%", pct)
	}
}

func TestViewerSearchEnterAndExit(t *testing.T) {
	t.Parallel()
	v := NewViewer("test", "# Hello\n\nHello world.")
	v.SetSize(80, 20)

	// / enters search mode
	v, _ = v.Update(tea.KeyPressMsg{Code: '/'})
	if !v.Searching() {
		t.Fatal("should be in search mode after /")
	}

	// esc exits search mode
	v, _ = v.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if v.Searching() {
		t.Error("should exit search mode after esc")
	}

	// f also enters search mode
	v, _ = v.Update(tea.KeyPressMsg{Code: 'f'})
	if !v.Searching() {
		t.Fatal("f should also enter search mode")
	}
}

func TestViewerApplySearch(t *testing.T) {
	t.Parallel()
	v := NewViewer("test", "# Hello\n\nHello world. Hello again.")
	v.SetSize(80, 20)

	// Directly set the search input value and call applySearch to test matching.
	v.search.input.SetValue("Hello")
	v.search.query = "Hello"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = applySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() == 0 {
		t.Error("should have matches for 'Hello'")
	}
}

func TestViewerApplySearchCaseInsensitive(t *testing.T) {
	t.Parallel()
	v := NewViewer("test", "# HELLO\n\nhello world.")
	v.SetSize(80, 20)

	v.search.query = "hello"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = applySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() < 2 {
		t.Errorf("case-insensitive search should find at least 2 matches, got %d", v.MatchCount())
	}
}

func TestViewerApplySearchEmpty(t *testing.T) {
	t.Parallel()
	v := NewViewer("test", "# Test")
	v.SetSize(80, 20)

	v.search.query = ""
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = applySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 0 {
		t.Error("empty query should have 0 matches")
	}
}

func TestViewerSearchScrollsToMatch(t *testing.T) {
	t.Parallel()
	// Build content long enough that "TARGET" is well below the viewport.
	var sb strings.Builder
	sb.WriteString("# Top\n\n")
	for i := range 60 {
		fmt.Fprintf(&sb, "Filler line %d\n\n", i)
	}
	sb.WriteString("TARGET appears here\n")

	v := NewViewer("test", sb.String())
	v.SetSize(80, 10) // short viewport

	// Initially at top.
	if v.vp.YOffset() != 0 {
		t.Fatalf("should start at top, got offset %d", v.vp.YOffset())
	}

	// Search for TARGET — should scroll down.
	v.search.query = "TARGET"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = applySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 1 {
		t.Fatalf("expected 1 match, got %d", v.MatchCount())
	}
	if v.vp.YOffset() == 0 {
		t.Error("viewport should have scrolled away from top to show the match")
	}
}

func TestViewerNextPrevScrolls(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("MATCH at top\n\n")
	for range 60 {
		sb.WriteString("filler\n\n")
	}
	sb.WriteString("MATCH at bottom\n")

	v := NewViewer("test", sb.String())
	v.SetSize(80, 10)

	v.search.query = "MATCH"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = applySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 2 {
		t.Fatalf("expected 2 matches, got %d", v.MatchCount())
	}

	startOffset := v.vp.YOffset()

	// n cycles to next match — should scroll.
	v, _ = v.Update(tea.KeyPressMsg{Code: 'n'})
	afterNext := v.vp.YOffset()

	if afterNext == startOffset {
		t.Error("n should scroll to a different position")
	}

	// N cycles back — should return.
	v, _ = v.Update(tea.KeyPressMsg{Code: 'N'})
	afterPrev := v.vp.YOffset()

	if afterPrev == afterNext {
		t.Error("N should scroll back")
	}
}

func TestSinglePageSearch(t *testing.T) {
	t.Parallel()
	tm := startSinglePage(t)

	// Enter search mode with /
	tm.Type("/")
	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		return bytes.Contains(bts, []byte("/"))
	}, teatest.WithDuration(testWait))

	// Type and confirm
	tm.Type("Hello")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})

	fm := finalModel(t, tm)
	if fm.search.searching {
		t.Error("should not be in search mode after enter")
	}
	if fm.search.query != "Hello" {
		t.Errorf("query should be 'Hello', got %q", fm.search.query)
	}
}
