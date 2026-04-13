package uikit

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// plain returns a render func that emits a single unstyled line.
func plain(text string) func(int) string {
	return func(_ int) string { return text }
}

// filledBody returns a body func that fills the given height with
// numbered lines ("line 0", "line 1", …).
func filledBody(_ int, h int) string {
	lines := make([]string, h)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return strings.Join(lines, "\n")
}

func TestChromeExactHeight(t *testing.T) {
	for _, h := range []int{10, 24, 40} {
		c := Chrome{
			Title:  plain("TITLE"),
			Status: plain("status"),
			Body:   filledBody,
		}
		out := c.Render(80, h)
		got := lipgloss.Height(out)
		if got != h {
			t.Errorf("height=%d: rendered %d lines, want %d", h, got, h)
		}
	}
}

func TestChromeBodyGetsRemainingHeight(t *testing.T) {
	var capturedH int
	c := Chrome{
		Title:  plain("T"), // 1 line
		Status: plain("S"), // 1 line
		Body: func(_ int, h int) string {
			capturedH = h
			return FitHeight("", h)
		},
	}
	c.Render(80, 20)
	if capturedH != 18 { // 20 - 1 title - 1 status
		t.Errorf("body got h=%d, want 18", capturedH)
	}
}

func TestChromeMultiLineTitle(t *testing.T) {
	var capturedH int
	c := Chrome{
		Title:  plain("LINE1\nLINE2"), // 2 lines
		Status: plain("S"),            // 1 line
		Body: func(_ int, h int) string {
			capturedH = h
			return FitHeight("", h)
		},
	}
	out := c.Render(80, 20)
	if capturedH != 17 { // 20 - 2 title - 1 status
		t.Errorf("body got h=%d, want 17", capturedH)
	}
	if got := lipgloss.Height(out); got != 20 {
		t.Errorf("total height=%d, want 20", got)
	}
}

func TestChromeZeroDimensions(t *testing.T) {
	c := Chrome{Title: plain("T"), Status: plain("S"), Body: filledBody}
	if out := c.Render(0, 20); out != "" {
		t.Errorf("zero width should return empty, got %q", out)
	}
	if out := c.Render(80, 0); out != "" {
		t.Errorf("zero height should return empty, got %q", out)
	}
}

func TestChromeBodyMinHeight(t *testing.T) {
	// Even when title+status would consume all rows, body gets at least 1 line.
	c := Chrome{
		Title:  plain("T"),
		Status: plain("S"),
		Body:   filledBody,
	}
	out := c.Render(80, 2) // only 2 lines total, 1 title + 1 status = 0 body → clamped to 1
	// Should not panic and should produce something.
	if lipgloss.Height(out) < 2 {
		t.Errorf("expected at least 2 lines, got %d", lipgloss.Height(out))
	}
}

// ── FitHeight ──────────────────────────────────────────────────────────

func TestFitHeightPads(t *testing.T) {
	out := FitHeight("a\nb", 5)
	if got := strings.Count(out, "\n") + 1; got != 5 {
		t.Errorf("FitHeight pad: got %d lines, want 5", got)
	}
}

func TestFitHeightTruncates(t *testing.T) {
	out := FitHeight("a\nb\nc\nd\ne", 3)
	if got := strings.Count(out, "\n") + 1; got != 3 {
		t.Errorf("FitHeight truncate: got %d lines, want 3", got)
	}
}

func TestFitHeightExact(t *testing.T) {
	out := FitHeight("a\nb\nc", 3)
	if got := strings.Count(out, "\n") + 1; got != 3 {
		t.Errorf("FitHeight exact: got %d lines, want 3", got)
	}
	if out != "a\nb\nc" {
		t.Errorf("FitHeight exact: content changed: %q", out)
	}
}

func TestFitHeightZero(t *testing.T) {
	if out := FitHeight("stuff", 0); out != "" {
		t.Errorf("FitHeight(0) should be empty, got %q", out)
	}
}
