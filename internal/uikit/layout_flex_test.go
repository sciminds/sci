package uikit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// ── VStack basics ─────────────────────────────────────────────────────────

func TestVStack_SingleFixed(t *testing.T) {
	t.Parallel()
	got := VStack(40, 10).
		Fixed(func(w int) string { return "hello" }).
		Render()

	if !strings.Contains(got, "hello") {
		t.Errorf("expected 'hello' in output, got %q", got)
	}
	// Total height should be exactly 10 lines.
	if h := lipgloss.Height(got); h != 10 {
		t.Errorf("height = %d, want 10", h)
	}
}

func TestVStack_FixedFlexFixed(t *testing.T) {
	t.Parallel()
	// Classic chrome: title (1 line) + flex body + status (1 line) = 20 total.
	var bodyW, bodyH int
	got := VStack(60, 20).
		Fixed(func(w int) string { return "TITLE" }).
		Flex(1, func(w, h int) string {
			bodyW, bodyH = w, h
			return strings.Repeat("x", w)
		}).
		Fixed(func(w int) string { return "STATUS" }).
		Render()

	if h := lipgloss.Height(got); h != 20 {
		t.Errorf("total height = %d, want 20", h)
	}
	// Body should get 20 - 1 (title) - 1 (status) = 18.
	if bodyH != 18 {
		t.Errorf("body height = %d, want 18", bodyH)
	}
	if bodyW != 60 {
		t.Errorf("body width = %d, want 60", bodyW)
	}
}

func TestVStack_MultipleFixed_NoFlex(t *testing.T) {
	t.Parallel()
	// Three fixed sections in a 10-high stack. Fixed sections take their
	// natural height; remaining space is blank filler at the bottom.
	got := VStack(40, 10).
		Fixed(func(w int) string { return "A" }).
		Fixed(func(w int) string { return "B" }).
		Fixed(func(w int) string { return "C" }).
		Render()

	if h := lipgloss.Height(got); h != 10 {
		t.Errorf("height = %d, want 10", h)
	}
	if !strings.Contains(got, "A") || !strings.Contains(got, "B") || !strings.Contains(got, "C") {
		t.Errorf("missing fixed content in %q", got)
	}
}

func TestVStack_MultilinFixed(t *testing.T) {
	t.Parallel()
	// A fixed section that renders 3 lines should consume 3 lines of budget.
	var bodyH int
	VStack(40, 20).
		Fixed(func(w int) string { return "line1\nline2\nline3" }).
		Flex(1, func(w, h int) string {
			bodyH = h
			return ""
		}).
		Render()

	// 20 total - 3 fixed = 17 flex.
	if bodyH != 17 {
		t.Errorf("body height = %d, want 17", bodyH)
	}
}

func TestVStack_MultipleFlex_Proportional(t *testing.T) {
	t.Parallel()
	// Two flex children with ratio 1:3 in a 20-high stack.
	var h1, h2 int
	VStack(40, 20).
		Flex(1, func(w, h int) string { h1 = h; return "" }).
		Flex(3, func(w, h int) string { h2 = h; return "" }).
		Render()

	// 20 total, ratio 1:3 → 5 and 15.
	if h1 != 5 {
		t.Errorf("flex(1) height = %d, want 5", h1)
	}
	if h2 != 15 {
		t.Errorf("flex(3) height = %d, want 15", h2)
	}
}

func TestVStack_Gap(t *testing.T) {
	t.Parallel()
	// Two fixed sections with a 2-line gap between them, total height 10.
	var bodyH int
	VStack(40, 10).
		Fixed(func(w int) string { return "TOP" }).
		Gap(2).
		Flex(1, func(w, h int) string { bodyH = h; return "" }).
		Render()

	// 10 total - 1 (fixed) - 2 (gap) = 7 flex.
	if bodyH != 7 {
		t.Errorf("flex height = %d, want 7", bodyH)
	}
}

func TestVStack_ZeroDimensions(t *testing.T) {
	t.Parallel()
	// Zero width or height should return empty string without panicking.
	got := VStack(0, 0).
		Fixed(func(w int) string { return "hello" }).
		Render()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestVStack_FlexMinHeight(t *testing.T) {
	t.Parallel()
	// Even if fixed sections consume all space, flex should get at least 1 line.
	var bodyH int
	VStack(40, 3).
		Fixed(func(w int) string { return "line1\nline2\nline3" }).
		Flex(1, func(w, h int) string { bodyH = h; return "" }).
		Render()

	if bodyH < 1 {
		t.Errorf("flex height = %d, want >= 1", bodyH)
	}
}

// ── HStack basics ─────────────────────────────────────────────────────────

func TestHStack_TwoFlexChildren(t *testing.T) {
	t.Parallel()
	var w1, w2 int
	got := HStack(80, 10).
		Flex(0.3, func(w, h int) string { w1 = w; return "LEFT" }).
		Flex(0.7, func(w, h int) string { w2 = w; return "RIGHT" }).
		Render()

	if w1 != 24 { // int(80 * 0.3) = 24
		t.Errorf("left width = %d, want 24", w1)
	}
	if w2 != 56 { // int(80 * 0.7) = 56
		t.Errorf("right width = %d, want 56", w2)
	}
	if h := lipgloss.Height(got); h != 10 {
		t.Errorf("height = %d, want 10", h)
	}
}

func TestHStack_Gap(t *testing.T) {
	t.Parallel()
	var w1, w2 int
	HStack(80, 10).
		Flex(0.5, func(w, h int) string { w1 = w; return "" }).
		Gap(2).
		Flex(0.5, func(w, h int) string { w2 = w; return "" }).
		Render()

	// Available = 80 - 2 (gap) = 78. Each gets int(78 * 0.5) = 39.
	if w1 != 39 {
		t.Errorf("left width = %d, want 39", w1)
	}
	if w2 != 39 {
		t.Errorf("right width = %d, want 39", w2)
	}
}

func TestHStack_FixedFlex(t *testing.T) {
	t.Parallel()
	// Fixed-width sidebar + flex main.
	var mainW int
	got := HStack(80, 10).
		Fixed(func(h int) string { return PadRight("NAV", 20) }).
		Flex(1, func(w, h int) string { mainW = w; return "MAIN" }).
		Render()

	// Fixed takes 20, flex gets 80 - 20 = 60.
	if mainW != 60 {
		t.Errorf("main width = %d, want 60", mainW)
	}
	if !strings.Contains(got, "NAV") || !strings.Contains(got, "MAIN") {
		t.Errorf("missing content in %q", got)
	}
}

func TestHStack_ZeroDimensions(t *testing.T) {
	t.Parallel()
	got := HStack(0, 0).
		Flex(1, func(w, h int) string { return "hello" }).
		Render()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ── Edge cases ────────────────────────────────────────────────────────────

func TestVStack_EmptyRender(t *testing.T) {
	t.Parallel()
	got := VStack(40, 10).Render()
	// No children → should still produce exactly 10 lines of empty space.
	if h := lipgloss.Height(got); h != 10 {
		t.Errorf("empty VStack height = %d, want 10", h)
	}
}

func TestHStack_EmptyRender(t *testing.T) {
	t.Parallel()
	got := HStack(40, 10).Render()
	if got != "" {
		t.Errorf("empty HStack = %q, want empty", got)
	}
}

func TestVStack_GapOnly(t *testing.T) {
	t.Parallel()
	// Gap with nothing else — gap consumes height budget.
	got := VStack(40, 5).
		Gap(2).
		Render()
	if h := lipgloss.Height(got); h != 5 {
		t.Errorf("height = %d, want 5", h)
	}
}

func TestVStack_FixedWidthPassthrough(t *testing.T) {
	t.Parallel()
	// Fixed callback should receive the stack's width.
	var gotW int
	VStack(42, 10).
		Fixed(func(w int) string { gotW = w; return "" }).
		Render()
	if gotW != 42 {
		t.Errorf("fixed width = %d, want 42", gotW)
	}
}

func TestHStack_FixedHeightPassthrough(t *testing.T) {
	t.Parallel()
	// HStack Fixed callback should receive the stack's height.
	var gotH int
	HStack(80, 15).
		Fixed(func(h int) string { gotH = h; return "" }).
		Render()
	if gotH != 15 {
		t.Errorf("fixed height = %d, want 15", gotH)
	}
}

func TestVStack_ContentFitHeight(t *testing.T) {
	t.Parallel()
	// Flex content that returns fewer lines than allocated should be padded
	// to exact height; content with more lines should be truncated.
	got := VStack(40, 5).
		Flex(1, func(w, h int) string { return "only one line" }).
		Render()
	if h := lipgloss.Height(got); h != 5 {
		t.Errorf("height = %d, want 5 (should pad)", h)
	}

	got2 := VStack(40, 3).
		Flex(1, func(w, h int) string { return "a\nb\nc\nd\ne" }).
		Render()
	if h := lipgloss.Height(got2); h != 3 {
		t.Errorf("height = %d, want 3 (should truncate)", h)
	}
}

func TestHStack_ContentFitWidth(t *testing.T) {
	t.Parallel()
	// Each flex child should be rendered at its allocated width.
	got := HStack(20, 1).
		Flex(0.5, func(w, h int) string { return "L" }).
		Flex(0.5, func(w, h int) string { return "R" }).
		Render()
	if w := lipgloss.Width(got); w != 20 {
		t.Errorf("width = %d, want 20", w)
	}
}
