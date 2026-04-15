package uikit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Shared sample cast for split view tests.
var splitSampleCast = []byte(`{"version": 2, "width": 80, "height": 24}
[0.5, "o", "$ "]
[1.0, "o", "ls\r\n"]
[1.5, "o", "file1.txt  file2.txt\r\n"]
`)

func splitTestCast() Cast {
	c, _ := ParseCast(splitSampleCast)
	return c
}

func newTestSplitAt(w, h int) *SplitView {
	viewer := NewMdViewer("test", "# Hello\n\nSome content here.")
	player := NewCastPlayer(splitTestCast(), 20)
	s := NewSplitView("test entry", viewer, player)
	s.SetSize(w, h)
	return s
}

func newTestSplit() *SplitView { return newTestSplitAt(100, 30) }

// ── splitPanelWidths edge cases ────────────────────────────────────────────

func TestSplitPanelWidths(t *testing.T) {
	tests := []struct {
		name   string
		totalW int
		castW  int
	}{
		{"normal 120x80", 120, 80},
		{"wide 200x80", 200, 80},
		{"narrow cast 120x40", 120, 40},
		{"matched 80x80", 80, 80},
		{"just above 2*min", splitMinPanelW*2 + splitDividerCols + 1, 80},
		{"exactly 2*min", splitMinPanelW*2 + splitDividerCols, 80},
		{"below 2*min", splitMinPanelW*2 + splitDividerCols - 1, 80},
		{"very narrow 20", 20, 80},
		{"tiny 5", 5, 80},
		{"divider only", splitDividerCols, 80},
		{"below divider", 2, 80},
		{"width 1", 1, 80},
		{"width 0", 0, 80},
		{"negative width", -5, 80},
		{"zero cast width", 120, 0},
		{"negative cast width", 120, -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left, right := splitPanelWidths(tt.totalW, tt.castW)
			if left < 1 || right < 1 {
				if tt.totalW-splitDividerCols >= 2 {
					t.Errorf("panel widths too small: left=%d, right=%d (totalW=%d)", left, right, tt.totalW)
				}
			}
			if tt.totalW-splitDividerCols >= 2 {
				if got := left + right + splitDividerCols; got != tt.totalW {
					t.Errorf("widths don't sum: left=%d + right=%d + %d = %d, want %d", left, right, splitDividerCols, got, tt.totalW)
				}
			}
		})
	}
}

func TestSplitPanelWidthsSymmetry(t *testing.T) {
	for totalW := splitDividerCols + 2; totalW <= 300; totalW++ {
		left, right := splitPanelWidths(totalW, 80)
		usable := totalW - splitDividerCols
		if left+right != usable {
			t.Fatalf("totalW=%d: left=%d + right=%d != usable=%d", totalW, left, right, usable)
		}
		if left < 1 || right < 1 {
			t.Fatalf("totalW=%d: panel went to zero: left=%d, right=%d", totalW, left, right)
		}
	}
}

// ── SetSize / View at various dimensions ───────────────────────────────────

func TestSplitViewSizesNoPanic(t *testing.T) {
	s := newTestSplit()
	s, _ = s.Update(CastTickMsg{Index: 0})

	dims := []struct {
		w, h int
	}{
		{200, 60}, {120, 40}, {100, 30}, {80, 24}, {60, 20}, {44, 15},
		{30, 10}, {20, 8}, {10, 5}, {5, 3}, {1, 1}, {0, 0},
	}

	for _, d := range dims {
		s.SetSize(d.w, d.h)
		view := s.View()
		if d.w < 1 || d.h < 1 {
			if view != "" {
				t.Errorf("%dx%d: expected empty view, got %d bytes", d.w, d.h, len(view))
			}
			continue
		}
		if view == "" {
			t.Errorf("%dx%d: expected non-empty view", d.w, d.h)
		}
	}
}

func TestSplitViewResizePreservesState(t *testing.T) {
	s := newTestSplit()

	s, _ = s.Update(CastTickMsg{Index: 0})
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeySpace})

	for _, w := range []int{200, 80, 40, 120} {
		s.SetSize(w, 30)
	}

	view := s.View()
	if !strings.Contains(view, "paused") {
		t.Error("player should still be paused after resizes")
	}
}

func TestSplitViewLineCount(t *testing.T) {
	sizes := []struct {
		w, h, maxLines int
	}{
		{120, 40, 50},
		{100, 25, 35},
		{80, 24, 34},
		{60, 15, 25},
	}
	for _, sz := range sizes {
		s := newTestSplitAt(sz.w, sz.h)
		s, _ = s.Update(CastTickMsg{Index: 0})
		view := s.View()
		lineCount := strings.Count(view, "\n") + 1
		if lineCount > sz.maxLines {
			t.Errorf("%dx%d: too many lines (%d), max %d", sz.w, sz.h, lineCount, sz.maxLines)
		}
	}
}

// ── Functional tests ───────────────────────────────────────────────────────

func TestSplitViewInit(t *testing.T) {
	s := newTestSplit()
	cmd := s.Init()
	if cmd == nil {
		t.Fatal("Init should return a tick cmd from player")
	}
}

func TestSplitViewPlayerControls(t *testing.T) {
	s := newTestSplit()

	s, _ = s.Update(CastTickMsg{Index: 0})
	view := s.View()
	if !strings.Contains(view, "playing") {
		t.Error("view should show playing status")
	}

	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	view = s.View()
	if !strings.Contains(view, "paused") {
		t.Error("view should show paused status after space")
	}

	s, _ = s.Update(tea.KeyPressMsg{Text: "r"})
	view = s.View()
	if strings.Contains(view, "paused") {
		t.Error("view should not show paused after restart")
	}
}

func TestSplitViewSearch(t *testing.T) {
	s := newTestSplit()

	s, _ = s.Update(tea.KeyPressMsg{Text: "/"})
	if !s.Searching() {
		t.Fatal("should be in search mode after /")
	}

	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.Searching() {
		t.Error("should not be searching after esc")
	}
}

func TestSplitViewContainsBothPanels(t *testing.T) {
	s := newTestSplit()
	s, _ = s.Update(CastTickMsg{Index: 0})

	view := s.View()
	if !strings.Contains(view, "test entry") {
		t.Error("view should contain the title")
	}
	if !strings.Contains(view, "┃") {
		t.Error("view should contain the thick border divider")
	}
	if !strings.Contains(view, "scroll") {
		t.Error("view should contain scroll hint")
	}
	if !strings.Contains(view, "pause/play") {
		t.Error("view should contain pause/play hint")
	}
}

// ── Stacked layout ─────────────────────────────────────────────────────────

func TestSplitViewStackedAtNarrowWidth(t *testing.T) {
	s := newTestSplitAt(60, 30)
	s, _ = s.Update(CastTickMsg{Index: 0})

	view := s.View()
	if !strings.Contains(view, "━") {
		t.Error("narrow view should contain horizontal divider ━")
	}
	if strings.Contains(view, "┃") {
		t.Error("narrow view should not contain vertical thick border ┃")
	}
	if !strings.Contains(view, "test entry") {
		t.Error("stacked view should still contain the title")
	}
}

func TestSplitViewSideBySideAtWideWidth(t *testing.T) {
	s := newTestSplitAt(120, 40)
	s, _ = s.Update(CastTickMsg{Index: 0})

	view := s.View()
	if !strings.Contains(view, "┃") {
		t.Error("wide view should contain vertical thick border ┃")
	}
}

func TestSplitViewLayoutTransition(t *testing.T) {
	s := newTestSplitAt(120, 40)
	s, _ = s.Update(CastTickMsg{Index: 0})

	view := s.View()
	if !strings.Contains(view, "┃") {
		t.Error("120-wide should be side-by-side")
	}

	s.SetSize(60, 30)
	view = s.View()
	if !strings.Contains(view, "━") {
		t.Error("60-wide should be stacked")
	}
	if strings.Contains(view, "┃") {
		t.Error("60-wide should not have vertical divider")
	}

	s.SetSize(120, 40)
	view = s.View()
	if !strings.Contains(view, "┃") {
		t.Error("back to 120-wide should be side-by-side again")
	}
}

func TestSplitViewFooterAdaptive(t *testing.T) {
	s := newTestSplitAt(120, 40)
	s, _ = s.Update(CastTickMsg{Index: 0})
	view := s.View()
	if !strings.Contains(view, "pause/play") {
		t.Error("wide footer should contain pause/play hint")
	}
	if !strings.Contains(view, "scroll") {
		t.Error("wide footer should contain scroll hint")
	}
	if !strings.Contains(view, "esc close") {
		t.Error("wide footer should always contain esc close")
	}

	s.SetSize(40, 15)
	view = s.View()
	if !strings.Contains(view, "esc close") {
		t.Error("narrow footer should still contain esc close")
	}
}
