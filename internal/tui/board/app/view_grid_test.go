package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
	"github.com/sciminds/cli/internal/tui/uikit"
)

// fixtureGridModel builds a Model with a synthetic two-column board ready
// for grid-render assertions, without spinning up a real Store.
func fixtureGridModel(width, height int) *Model {
	m := &Model{
		styles:    ui.TUI,
		screen:    screenGrid,
		cur:       uikit.Grid2D{Col: 0, Row: -1},
		width:     width,
		height:    height,
		collapsed: map[string]bool{},
		router:    buildRouter(),
		current: engine.Board{
			BoardMeta: engine.BoardMeta{
				ID:    "alpha",
				Title: "Alpha",
				Columns: []engine.Column{
					{ID: "todo", Title: "To Do"},
					{ID: "done", Title: "Done"},
				},
			},
			Cards: []engine.Card{
				{ID: "c1", Title: "Write tests", Column: "todo", Position: 1},
				{ID: "c2", Title: "Ship it", Column: "todo", Position: 2},
				{ID: "c3", Title: "Done already", Column: "done", Position: 1},
			},
		},
	}
	return m
}

// fixtureCalendarModel builds a Model with a 12-column "calendar" board.
// Used for horizontal-scroll and collapse tests.
func fixtureCalendarModel(width, height int) *Model {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	cols := make([]engine.Column, len(months))
	for i, mo := range months {
		cols[i] = engine.Column{ID: strings.ToLower(mo), Title: mo}
	}
	m := &Model{
		styles:    ui.TUI,
		screen:    screenGrid,
		cur:       uikit.Grid2D{Col: 0, Row: -1},
		width:     width,
		height:    height,
		collapsed: map[string]bool{},
		router:    buildRouter(),
		current: engine.Board{
			BoardMeta: engine.BoardMeta{
				ID:      "calendar",
				Title:   "Calendar",
				Columns: cols,
			},
		},
	}
	return m
}

// TestRenderColumnLineCount measures a single column in isolation: a
// column rendered with interior height N must produce exactly N+2 lines
// (top border + N body rows + bottom border).
func TestRenderColumnLineCount(t *testing.T) {
	m := fixtureGridModel(100, 30)
	cards := m.cardsByColumn()["todo"]
	for _, interiorH := range []int{5, 10, 26, 40} {
		out := m.renderColumn(m.current.Columns[0], cards, 0, 37, interiorH)
		got := strings.Count(out, "\n") + 1
		want := interiorH + 2
		if got != want {
			t.Errorf("interiorH=%d: column rendered %d lines, want %d", interiorH, got, want)
		}
	}
}

// TestGridFitsTerminalHeight asserts that the rendered top-level view is
// exactly m.height lines tall — no overflow, no missing rows.
func TestGridFitsTerminalHeight(t *testing.T) {
	cases := []struct{ w, h int }{
		{80, 24},
		{100, 30},
		{120, 40},
		{60, 12},
	}
	for _, tc := range cases {
		m := fixtureGridModel(tc.w, tc.h)
		out := m.buildView()
		got := lipgloss.Height(out)
		if got != tc.h {
			t.Errorf("term %dx%d: rendered height = %d, want %d", tc.w, tc.h, got, tc.h)
		}
		if w := lipgloss.Width(out); w > tc.w {
			t.Errorf("term %dx%d: rendered width = %d, want <= %d", tc.w, tc.h, w, tc.w)
		}
	}
}

// TestDetailFitsTerminalHeight verifies the detail-view frame doesn't
// overflow vertically. DetailFrame uses Padding(1, 2), so any height
// accounting that ignores the vertical padding rows would push the bottom
// border off-screen.
func TestDetailFitsTerminalHeight(t *testing.T) {
	cases := []struct{ w, h int }{
		{80, 24},
		{100, 30},
		{60, 12},
	}
	for _, tc := range cases {
		m := fixtureGridModel(tc.w, tc.h)
		m.screen = screenDetail
		m.cur = uikit.Grid2D{Col: 0, Row: 0}
		out := m.buildView()
		if got := lipgloss.Height(out); got != tc.h {
			t.Errorf("detail term %dx%d: rendered height = %d, want %d", tc.w, tc.h, got, tc.h)
		}
		// Bottom border ╰─╯ must appear on the row above the status bar.
		lines := strings.Split(out, "\n")
		last := lines[len(lines)-2]
		if !strings.Contains(last, "╰") || !strings.Contains(last, "╯") {
			t.Errorf("detail %dx%d: bottom border missing from last body row: %q", tc.w, tc.h, last)
		}
	}
}

// TestGridColumnBottomBorderVisible asserts that the LAST body row contains
// the column bottom-border glyphs for every column. The card-level bottom
// border ╰─╯ would also satisfy a loose check, so we anchor on the row that
// must close the columns: the second-to-last row of the full view.
func TestGridColumnBottomBorderVisible(t *testing.T) {
	m := fixtureGridModel(100, 30)
	out := m.buildView()
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered view too short: %d lines", len(lines))
	}
	// Last body row sits just above the status bar.
	last := lines[len(lines)-2]
	if !strings.Contains(last, "╰") || !strings.Contains(last, "╯") {
		t.Errorf("column bottom border missing from last body row.\nrow: %q\n--- full view ---\n%s", last, out)
	}
	// Each column must contribute a ╰…╯ pair on that row.
	wantPairs := len(m.current.Columns)
	if got := strings.Count(last, "╯"); got != wantPairs {
		t.Errorf("expected %d ╯ on closing row, got %d (row=%q)", wantPairs, got, last)
	}
}

// ── Horizontal scroll + collapse ────────────────────────────────────────

// TestVisibleColumnRangeFitsAll: when every column fits at the minimum
// width, visibleColumnRange returns the full range and no scrolling is
// needed.
func TestVisibleColumnRangeFitsAll(t *testing.T) {
	m := fixtureGridModel(100, 30)
	start, end := m.visibleColumnRange(100)
	if start != 0 || end != 2 {
		t.Errorf("fit: want [0,2), got [%d,%d)", start, end)
	}
}

// TestVisibleColumnRangeWindows: with 12 calendar columns at width 80,
// only a subset is visible and the window starts at gridScroll.
func TestVisibleColumnRangeWindows(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	start, end := m.visibleColumnRange(80)
	if start != 0 {
		t.Errorf("start=%d, want 0", start)
	}
	if end >= 12 {
		t.Errorf("end=%d, want < 12 (windowed mode)", end)
	}
	if end-start < 2 {
		t.Errorf("only %d visible columns, want >= 2", end-start)
	}
	// Shifting gridScroll moves the window right.
	m.gridScroll = 4
	start, end = m.visibleColumnRange(80)
	if start != 4 {
		t.Errorf("start=%d after gridScroll=4, want 4", start)
	}
	if end <= 4 {
		t.Errorf("end=%d, window should contain >=1 column", end)
	}
}

// TestEnsureCursorVisibleScrollsRight: moving cur.col past the right edge
// of the window bumps gridScroll so the cursor lands in view.
func TestEnsureCursorVisibleScrollsRight(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	m.cur.Col = 8
	m.ensureCursorVisible(80)
	start, end := m.visibleColumnRange(80)
	if start > 8 || end <= 8 {
		t.Errorf("cur.col=8 not in window [%d,%d)", start, end)
	}
}

// TestEnsureCursorVisibleScrollsLeft: moving cur.col to a column below
// gridScroll pulls the window left.
func TestEnsureCursorVisibleScrollsLeft(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	m.gridScroll = 6
	m.cur.Col = 1
	m.ensureCursorVisible(80)
	start, end := m.visibleColumnRange(80)
	if start > 1 || end <= 1 {
		t.Errorf("cur.col=1 not in window [%d,%d)", start, end)
	}
}

// TestToggleCollapseCurrent: `c` toggles collapse for the focused column.
func TestToggleCollapseCurrent(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	m.cur.Col = 2 // Mar
	m.toggleCollapseCurrent()
	if !m.collapsed["mar"] {
		t.Errorf("expected mar collapsed after toggle")
	}
	m.toggleCollapseCurrent()
	if m.collapsed["mar"] {
		t.Errorf("expected mar expanded after second toggle")
	}
}

// TestExpandAllClearsCollapse: `C` clears the collapse map.
func TestExpandAllClearsCollapse(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	m.collapsed["jan"] = true
	m.collapsed["feb"] = true
	m.expandAll()
	if len(m.collapsed) != 0 {
		t.Errorf("expected empty collapsed map, got %v", m.collapsed)
	}
}

// TestCollapseFreesSpace: collapsing leading columns frees room so more
// columns fit in the same terminal width.
func TestCollapseFreesSpace(t *testing.T) {
	m := fixtureCalendarModel(80, 30)
	_, endBefore := m.visibleColumnRange(80)
	m.collapsed["jan"] = true
	m.collapsed["feb"] = true
	m.collapsed["mar"] = true
	m.collapsed["apr"] = true
	_, endAfter := m.visibleColumnRange(80)
	if endAfter <= endBefore {
		t.Errorf("collapse should show more columns: before=%d after=%d", endBefore, endAfter)
	}
}

// ── j/k cycling within a column ─────────────────────────────────────────

// pressKey dispatches a single-rune key through handleGridKey for unit
// tests that don't need the full teatest loop.
func pressKey(m *Model, k string) {
	r := []rune(k)[0]
	m.handleGridKey(tea.KeyPressMsg{Code: r, Text: k})
}

// TestGridJWrapsAtLastCard: j on the last card in a column wraps to the
// first. fixtureGridModel's "todo" column has two cards (c1, c2).
func TestGridJWrapsAtLastCard(t *testing.T) {
	m := fixtureGridModel(100, 30)
	m.cur = uikit.Grid2D{Col: 0, Row: 1} // on c2 (last)
	pressKey(m, "j")
	if m.cur.Row != 0 {
		t.Errorf("cur.card=%d after j-wrap, want 0", m.cur.Row)
	}
}

// TestGridKWrapsAtFirstCard: k on the first card in a column wraps to
// the last.
func TestGridKWrapsAtFirstCard(t *testing.T) {
	m := fixtureGridModel(100, 30)
	m.cur = uikit.Grid2D{Col: 0, Row: 0} // on c1 (first)
	pressKey(m, "k")
	if m.cur.Row != 1 {
		t.Errorf("cur.card=%d after k-wrap, want 1", m.cur.Row)
	}
}

// TestGridKFromUnfocusedGoesToLast: when no card is focused (-1),
// pressing k jumps to the last card in the column — more useful than
// a no-op.
func TestGridKFromUnfocusedGoesToLast(t *testing.T) {
	m := fixtureGridModel(100, 30)
	m.cur = uikit.Grid2D{Col: 0, Row: -1}
	pressKey(m, "k")
	if m.cur.Row != 1 {
		t.Errorf("cur.card=%d after k-from-unfocused, want 1", m.cur.Row)
	}
}

// TestGridJKEmptyColumnNoop: j/k on an empty column is a no-op and does
// not panic.
func TestGridJKEmptyColumnNoop(t *testing.T) {
	m := fixtureGridModel(100, 30)
	// Drop all cards so both columns are empty.
	m.current.Cards = nil
	m.cur = uikit.Grid2D{Col: 0, Row: -1}
	pressKey(m, "j")
	pressKey(m, "k")
	if m.cur.Row != -1 {
		t.Errorf("cur.card=%d on empty column, want -1", m.cur.Row)
	}
}

// TestCalendarBuildViewFitsHeight: the calendar grid renders to exact
// terminal height regardless of scroll or collapse state.
func TestCalendarBuildViewFitsHeight(t *testing.T) {
	cases := []struct {
		name      string
		collapsed []string
		scroll    int
	}{
		{"noCollapse", nil, 0},
		{"someCollapsed", []string{"jan", "feb", "mar"}, 0},
		{"scrolled", nil, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureCalendarModel(80, 24)
			for _, id := range tc.collapsed {
				m.collapsed[id] = true
			}
			m.gridScroll = tc.scroll
			out := m.buildView()
			if got := lipgloss.Height(out); got != 24 {
				t.Errorf("height=%d, want 24", got)
			}
			if w := lipgloss.Width(out); w > 80 {
				t.Errorf("width=%d, want <= 80", w)
			}
		})
	}
}
