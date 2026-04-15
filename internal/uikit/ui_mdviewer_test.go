package uikit

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMdViewerScrollPercent(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# Test\n\nLine 1\nLine 2\nLine 3")
	v.SetSize(80, 50)
	pct := v.ScrollPercent()
	if pct != 100 {
		t.Errorf("short content should be 100%%, got %d%%", pct)
	}
}

func TestMdViewerSearchEnterAndExit(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# Hello\n\nHello world.")
	v.SetSize(80, 20)

	v, _ = v.Update(tea.KeyPressMsg{Code: '/'})
	if !v.Searching() {
		t.Fatal("should be in search mode after /")
	}

	v, _ = v.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if v.Searching() {
		t.Error("should exit search mode after esc")
	}

	v, _ = v.Update(tea.KeyPressMsg{Code: 'f'})
	if !v.Searching() {
		t.Fatal("f should also enter search mode")
	}
}

func TestMdViewerApplySearch(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# Hello\n\nHello world. Hello again.")
	v.SetSize(80, 20)

	v.search.input.SetValue("Hello")
	v.search.query = "Hello"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = mdApplySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() == 0 {
		t.Error("should have matches for 'Hello'")
	}
}

func TestMdViewerApplySearchCaseInsensitive(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# HELLO\n\nhello world.")
	v.SetSize(80, 20)

	v.search.query = "hello"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = mdApplySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() < 2 {
		t.Errorf("case-insensitive search should find at least 2 matches, got %d", v.MatchCount())
	}
}

func TestMdViewerApplySearchEmpty(t *testing.T) {
	t.Parallel()
	v := NewMdViewer("test", "# Test")
	v.SetSize(80, 20)

	v.search.query = ""
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = mdApplySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 0 {
		t.Error("empty query should have 0 matches")
	}
}

func TestMdViewerSearchScrollsToMatch(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("# Top\n\n")
	for i := range 60 {
		fmt.Fprintf(&sb, "Filler line %d\n\n", i)
	}
	sb.WriteString("TARGET appears here\n")

	v := NewMdViewer("test", sb.String())
	v.SetSize(80, 10)

	if v.vp.YOffset() != 0 {
		t.Fatalf("should start at top, got offset %d", v.vp.YOffset())
	}

	v.search.query = "TARGET"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = mdApplySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 1 {
		t.Fatalf("expected 1 match, got %d", v.MatchCount())
	}
	if v.vp.YOffset() == 0 {
		t.Error("viewport should have scrolled away from top to show the match")
	}
}

func TestMdViewerNextPrevScrolls(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("MATCH at top\n\n")
	for range 60 {
		sb.WriteString("filler\n\n")
	}
	sb.WriteString("MATCH at bottom\n")

	v := NewMdViewer("test", sb.String())
	v.SetSize(80, 10)

	v.search.query = "MATCH"
	v.search.matchLines, v.search.matchCount, v.search.matchIdx = mdApplySearch(v.search.query, v.rendered, &v.vp)

	if v.MatchCount() != 2 {
		t.Fatalf("expected 2 matches, got %d", v.MatchCount())
	}

	startOffset := v.vp.YOffset()

	v, _ = v.Update(tea.KeyPressMsg{Code: 'n'})
	afterNext := v.vp.YOffset()

	if afterNext == startOffset {
		t.Error("n should scroll to a different position")
	}

	v, _ = v.Update(tea.KeyPressMsg{Code: 'N'})
	afterPrev := v.vp.YOffset()

	if afterPrev == afterNext {
		t.Error("N should scroll back")
	}
}
