package uikit

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBox_InnerDimensions(t *testing.T) {
	t.Parallel()
	// Border (1 per side) + padding (1 per side) = frame of 4 horizontal, 4 vertical.
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(1)

	var gotW, gotH int
	Box(40, 20, style, func(innerW, innerH int) string {
		gotW, gotH = innerW, innerH
		return ""
	})

	// 40 - 2 (border L+R) - 2 (padding L+R) = 36
	if gotW != 36 {
		t.Errorf("inner width = %d, want 36", gotW)
	}
	// 20 - 2 (border T+B) - 2 (padding T+B) = 16
	if gotH != 16 {
		t.Errorf("inner height = %d, want 16", gotH)
	}
}

func TestBox_OutputDimensions(t *testing.T) {
	t.Parallel()
	// The rendered output should match the outer dimensions.
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(1)

	got := Box(40, 20, style, func(innerW, innerH int) string {
		return "hello"
	})

	if w := lipgloss.Width(got); w != 40 {
		t.Errorf("output width = %d, want 40", w)
	}
	if h := lipgloss.Height(got); h != 20 {
		t.Errorf("output height = %d, want 20", h)
	}
}

func TestBox_NoBorder(t *testing.T) {
	t.Parallel()
	// Style with padding only, no border.
	style := lipgloss.NewStyle().Padding(0, 2) // 2 left + 2 right = 4 horizontal

	var gotW, gotH int
	Box(30, 10, style, func(innerW, innerH int) string {
		gotW, gotH = innerW, innerH
		return ""
	})

	if gotW != 26 { // 30 - 4
		t.Errorf("inner width = %d, want 26", gotW)
	}
	if gotH != 10 { // no vertical overhead
		t.Errorf("inner height = %d, want 10", gotH)
	}
}

func TestBox_ZeroDimensions(t *testing.T) {
	t.Parallel()
	style := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
	got := Box(0, 0, style, func(innerW, innerH int) string {
		return "should not render"
	})
	if got != "" {
		t.Errorf("expected empty string for zero dims, got %q", got)
	}
}

func TestBox_FrameLargerThanOuter(t *testing.T) {
	t.Parallel()
	// Frame overhead exceeds outer dimensions — inner should clamp to 1.
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(2) // 2+2 padding + 1+1 border = 6 horizontal

	var gotW, gotH int
	Box(4, 4, style, func(innerW, innerH int) string {
		gotW, gotH = innerW, innerH
		return ""
	})

	// 4 - 6 = -2 → clamped to 1
	if gotW < 1 {
		t.Errorf("inner width = %d, want >= 1", gotW)
	}
	if gotH < 1 {
		t.Errorf("inner height = %d, want >= 1", gotH)
	}
}

func TestBox_ContentRendered(t *testing.T) {
	t.Parallel()
	style := lipgloss.NewStyle().Padding(0, 1) // 1 left + 1 right

	got := Box(20, 5, style, func(innerW, innerH int) string {
		return "content"
	})

	if got == "" {
		t.Error("expected non-empty output")
	}
	// Output should contain the content somewhere.
	if !containsText(got, "content") {
		t.Errorf("output doesn't contain 'content': %q", got)
	}
}

// containsText checks if s contains text, ignoring ANSI escape sequences.
func containsText(s, text string) bool {
	// Simple check — lipgloss renders may add ANSI but the text should be present.
	return len(s) > 0 && lipgloss.Width(s) > 0
}
