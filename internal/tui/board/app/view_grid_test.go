package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
)

// fixtureGridModel builds a Model with a synthetic two-column board ready
// for grid-render assertions, without spinning up a real Store.
func fixtureGridModel(width, height int) *Model {
	m := &Model{
		styles: ui.TUI,
		screen: screenGrid,
		cur:    cursor{col: 0, card: -1},
		width:  width,
		height: height,
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
		m.cur = cursor{col: 0, card: 0}
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
