package ui

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/tui/uikit"
)

func TestTUISingletonNotNil(t *testing.T) {
	if TUI == nil {
		t.Fatal("TUI singleton is nil")
	}
}

func TestStyleAccessorsNonZero(t *testing.T) {
	// Verify key style accessors return initialized styles with structural
	// effects (border, padding, background) that render even without a TTY.
	// Foreground-only styles are skipped since lipgloss elides ANSI in non-TTY.
	accessors := []struct {
		name string
		fn   func() string
	}{
		{"OverlayBox", func() string { return TUI.OverlayBox().Render("x") }},
		{"Keycap", func() string { return TUI.Keycap().Render("x") }},
		{"HeaderSection", func() string { return TUI.HeaderSection().Render("x") }},
	}
	for _, tt := range accessors {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got == "x" {
				t.Errorf("%s().Render(\"x\") returned plain \"x\" — style has no effect", tt.name)
			}
		})
	}
}

func TestOverlayViewAtZeroSize(t *testing.T) {
	o := uikit.NewOverlay("title", "content", 0, 0)
	_ = o.View() // must not panic
}

func TestOverlayWidth(t *testing.T) {
	tests := []struct {
		name            string
		termW, min, max int
		want            int
	}{
		{"clamps to min", 10, 30, 80, 30},
		{"clamps to max", 200, 30, 80, 80},
		{"within range", 60, 30, 80, 60 - uikit.OverlayMargin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uikit.OverlayWidth(tt.termW, tt.min, tt.max)
			if got != tt.want {
				t.Errorf("OverlayWidth(%d, %d, %d) = %d, want %d",
					tt.termW, tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestDimBackground(t *testing.T) {
	result := uikit.DimBackground("hello")
	if !strings.Contains(result, "\033[2m") {
		t.Error("DimBackground should contain SGR 2 (faint)")
	}
}

func TestCancelFaint(t *testing.T) {
	result := uikit.CancelFaint("hello")
	if !strings.Contains(result, "\033[22m") {
		t.Error("CancelFaint should contain SGR 22 (cancel faint)")
	}
}

func TestWordWrap(t *testing.T) {
	got := uikit.WordWrap("one two three four five", 10)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Errorf("expected wrapping, got %d lines: %q", len(lines), got)
	}
}
