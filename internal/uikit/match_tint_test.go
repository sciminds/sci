package uikit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestMatchTint_NonEmpty(t *testing.T) {
	t.Parallel()
	s := NewStyles(true)
	tint := s.MatchTint()

	// Background must be set (non-default).
	rendered := tint.Render("hello")
	if rendered == "hello" {
		t.Fatal("MatchTint should emit ANSI escapes, got plain text")
	}
	// Inherit from a foreground-only base — composed output should keep the
	// foreground and add the tint background.
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("#112233")).Bold(true)
	composed := tint.Inherit(base).Render("x")
	if !strings.Contains(composed, "x") {
		t.Fatalf("composed render must retain content, got %q", composed)
	}
}

func TestOverlaySearch_UsesMatchRowTokens(t *testing.T) {
	t.Parallel()
	content := "foo line\nbar line\nfoobar line\nunrelated\n"
	o := NewOverlay("t", content, 80, 40)

	// Enter search mode and drive a two-token query.
	o, _ = o.Update(tea.KeyPressMsg{Code: '/'})
	o.search.input.SetValue("foo bar")
	o.search.liveUpdate(tea.KeyPressMsg{Code: 'r'}, &o.vp, o.rendered)
	o, _ = o.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Both tokens should have been found on their own lines (3 match-lines:
	// "foo line", "bar line", "foobar line"). A phrase-only matcher would
	// miss the first two.
	if o.search.matchCount < 3 {
		t.Errorf("expected >=3 token matches (foo + bar + foobar), got %d", o.search.matchCount)
	}
}

func TestOverlay_WithInitialQuerySeedsHighlights(t *testing.T) {
	t.Parallel()
	o := NewOverlay("t", "hello world hello", 80, 40, WithInitialQuery("hello world"))
	if o.search.query != "hello world" {
		t.Errorf("initial query not seeded, got %q", o.search.query)
	}
	if o.search.matchCount == 0 {
		t.Error("expected highlights seeded on first render")
	}
	view := o.View()
	if !strings.Contains(view, "\x1b[7m") {
		t.Error("first View() should already contain reverse-video highlights")
	}
}

func TestOverlay_QuotedPhraseHighlightsContiguous(t *testing.T) {
	t.Parallel()
	// Content has one contiguous "gossip drives" run and one scattered
	// "gossip about drives". The phrase query should highlight only the
	// contiguous run — same semantics as row-level phrase search.
	content := "gossip drives deposition\ngossip about drives\n"
	o := NewOverlay("t", content, 80, 40, WithInitialQuery(`"gossip drives"`))
	if o.search.matchCount != 1 {
		t.Errorf("expected 1 contiguous phrase match, got %d", o.search.matchCount)
	}
}

func TestMarkdownOverlay_WithInitialQuerySeedsHighlights(t *testing.T) {
	t.Parallel()
	o := NewMarkdownOverlay("t", "hello world hello", 80, 40, WithInitialQuery("hello"))
	if o.search.query != "hello" {
		t.Errorf("initial query not seeded, got %q", o.search.query)
	}
	if o.search.matchCount == 0 {
		t.Error("expected highlights seeded on first render")
	}
}
