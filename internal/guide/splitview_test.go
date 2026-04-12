package guide

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/mdview"
)

// ── Helpers ────────────────────────────────────────────────────────────────

func newTestSplitAt(w, h int) *splitView {
	viewer := mdview.NewViewer("test", "# Hello\n\nSome content here.")
	player := NewPlayer(testCast(), 20)
	s := newSplitView("test entry", viewer, player)
	s.SetSize(w, h)
	return s
}

func newTestSplit() *splitView {
	return newTestSplitAt(testTermW, testTermH)
}

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
		{"just above 2*min", minPanelW*2 + dividerCols + 1, 80},
		{"exactly 2*min", minPanelW*2 + dividerCols, 80},
		{"below 2*min", minPanelW*2 + dividerCols - 1, 80},
		{"very narrow 20", 20, 80},
		{"tiny 5", 5, 80},
		{"divider only", dividerCols, 80},
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
				// Must always be at least 1 column each.
				if tt.totalW-dividerCols >= 2 {
					t.Errorf("panel widths too small: left=%d, right=%d (totalW=%d)", left, right, tt.totalW)
				}
			}
			if tt.totalW-dividerCols >= 2 {
				if got := left + right + dividerCols; got != tt.totalW {
					t.Errorf("widths don't sum: left=%d + right=%d + %d = %d, want %d", left, right, dividerCols, got, tt.totalW)
				}
			}
		})
	}
}

func TestSplitPanelWidthsSymmetry(t *testing.T) {
	// At any usable width the 45% cap should keep the right panel from
	// dominating, and minPanelW should keep it from vanishing.
	for totalW := dividerCols + 2; totalW <= 300; totalW++ {
		left, right := splitPanelWidths(totalW, 80)
		usable := totalW - dividerCols
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
	// Build the split once then resize through a range of dimensions.
	// None of these should panic or produce empty output at usable sizes.
	s := newTestSplit()
	s, _ = s.Update(TickMsg{Index: 0}) // advance player so it has output

	dims := []struct {
		w, h int
	}{
		{200, 60}, // large
		{120, 40}, // standard
		{100, 30}, // default test
		{80, 24},  // classic terminal
		{60, 20},  // smallish
		{44, 15},  // minimum-ish
		{30, 10},  // very small
		{20, 8},   // tiny
		{10, 5},   // extreme
		{5, 3},    // near-impossible
		{1, 1},    // degenerate
		{0, 0},    // zero — should return ""
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

	// Advance player and pause
	s, _ = s.Update(TickMsg{Index: 0})
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeySpace})

	// Resize several times
	for _, w := range []int{200, 80, 40, 120} {
		s.SetSize(w, 30)
	}

	// Player state should be preserved
	view := s.View()
	if !strings.Contains(view, "paused") {
		t.Error("player should still be paused after resizes")
	}
}

func TestSplitViewLineCount(t *testing.T) {
	// The rendered view should not have wildly more lines than the terminal
	// height — lipgloss JoinHorizontal pads to the taller panel, plus we
	// have a few chrome lines.
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
		s, _ = s.Update(TickMsg{Index: 0})
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

	// Advance the player
	s, _ = s.Update(TickMsg{Index: 0})
	view := s.View()
	if !strings.Contains(view, "playing") {
		t.Error("view should show playing status")
	}

	// Pause
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	view = s.View()
	if !strings.Contains(view, "paused") {
		t.Error("view should show paused status after space")
	}

	// Restart
	s, _ = s.Update(tea.KeyPressMsg{Text: "r"})
	view = s.View()
	if strings.Contains(view, "paused") {
		t.Error("view should not show paused after restart")
	}
}

func TestSplitViewSearch(t *testing.T) {
	s := newTestSplit()

	// Enter search mode
	s, _ = s.Update(tea.KeyPressMsg{Text: "/"})
	if !s.Searching() {
		t.Fatal("should be in search mode after /")
	}

	// Cancel search
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.Searching() {
		t.Error("should not be searching after esc")
	}
}

func TestSplitViewContainsBothPanels(t *testing.T) {
	s := newTestSplit()

	// Advance player so it has output
	s, _ = s.Update(TickMsg{Index: 0})

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

// ── Stacked layout ────────────────────────────────────────────────────────

func TestSplitViewStackedAtNarrowWidth(t *testing.T) {
	// Below splitMinW the view should use a stacked layout with a
	// horizontal divider (━) instead of the vertical thick border (┃).
	s := newTestSplitAt(60, 30)
	s, _ = s.Update(TickMsg{Index: 0})

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
	s, _ = s.Update(TickMsg{Index: 0})

	view := s.View()
	if !strings.Contains(view, "┃") {
		t.Error("wide view should contain vertical thick border ┃")
	}
}

func TestSplitViewLayoutTransition(t *testing.T) {
	// Resize from wide → narrow → wide and verify layout switches.
	s := newTestSplitAt(120, 40)
	s, _ = s.Update(TickMsg{Index: 0})

	// Wide → side-by-side
	view := s.View()
	if !strings.Contains(view, "┃") {
		t.Error("120-wide should be side-by-side")
	}

	// Narrow → stacked
	s.SetSize(60, 30)
	view = s.View()
	if !strings.Contains(view, "━") {
		t.Error("60-wide should be stacked")
	}
	if strings.Contains(view, "┃") {
		t.Error("60-wide should not have vertical divider")
	}

	// Back to wide → side-by-side again
	s.SetSize(120, 40)
	view = s.View()
	if !strings.Contains(view, "┃") {
		t.Error("back to 120-wide should be side-by-side again")
	}
}

// ── Adaptive footer ──────────────────────────────────────────────────────

func TestSplitViewFooterAdaptive(t *testing.T) {
	// At full width all hints should appear.
	s := newTestSplitAt(120, 40)
	s, _ = s.Update(TickMsg{Index: 0})
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

	// At very narrow width some hints should be dropped but esc always present.
	s.SetSize(40, 15)
	view = s.View()
	if !strings.Contains(view, "esc close") {
		t.Error("narrow footer should still contain esc close")
	}
}

// ── Integration: resize during split view ──────────────────────────────────

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

	// Enter book and open split
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")
	tSendSpecial(tm, tea.KeyEnter)
	tWaitForOutput(t, tm, "split test")

	// Resize to various dimensions while in split view
	for _, size := range []tea.WindowSizeMsg{
		{Width: 200, Height: 50},
		{Width: 80, Height: 24},
		{Width: 50, Height: 15},
		{Width: 120, Height: 40},
	} {
		tm.Send(size)
	}

	// Should still be in split mode, not crashed
	fm := tFinalModel(t, tm)
	if fm.level != levelSplit {
		t.Errorf("should still be at split level after resizes, got %d", fm.level)
	}
	if fm.split == nil {
		t.Fatal("split should be non-nil after resizes")
	}
}

// ── Integration: open/close split view ─────────────────────────────────────

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
