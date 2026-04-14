package uikit

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestGrid_EqualColumns(t *testing.T) {
	t.Parallel()
	// 3 cells in a 3-column grid → 1 row. Each cell should get 60/3 = 20 width.
	var widths [3]int
	Grid(60, 10, 3).
		Cell(func(w, h int) string { widths[0] = w; return "" }).
		Cell(func(w, h int) string { widths[1] = w; return "" }).
		Cell(func(w, h int) string { widths[2] = w; return "" }).
		Render()

	for i, w := range widths {
		if w != 20 {
			t.Errorf("cell[%d] width = %d, want 20", i, w)
		}
	}
}

func TestGrid_GapSubtracted(t *testing.T) {
	t.Parallel()
	// 3 columns with gap=2 in 66 width.
	// Available = 66 - 2*2 (gaps between 3 cols) = 62. Each cell = 62/3 = 20 (last gets 22).
	var widths [3]int
	Grid(66, 10, 3).
		Gap(2).
		Cell(func(w, h int) string { widths[0] = w; return "" }).
		Cell(func(w, h int) string { widths[1] = w; return "" }).
		Cell(func(w, h int) string { widths[2] = w; return "" }).
		Render()

	// Available after gaps: 66 - 4 = 62. 62/3 = 20 remainder 2.
	// First two get 20, last gets remainder.
	if widths[0] != 20 {
		t.Errorf("cell[0] width = %d, want 20", widths[0])
	}
	if widths[1] != 20 {
		t.Errorf("cell[1] width = %d, want 20", widths[1])
	}
	// Last column gets remaining: 62 - 20*2 = 22.
	if widths[2] != 22 {
		t.Errorf("cell[2] width = %d, want 22", widths[2])
	}
}

func TestGrid_MultipleRows(t *testing.T) {
	t.Parallel()
	// 5 cells in a 3-column grid → 2 rows.
	var heights [5]int
	got := Grid(60, 20, 3).
		Cell(func(w, h int) string { heights[0] = h; return "" }).
		Cell(func(w, h int) string { heights[1] = h; return "" }).
		Cell(func(w, h int) string { heights[2] = h; return "" }).
		Cell(func(w, h int) string { heights[3] = h; return "" }).
		Cell(func(w, h int) string { heights[4] = h; return "" }).
		Render()

	// 2 rows in 20 height: each row gets 10.
	for i, h := range heights {
		if h != 10 {
			t.Errorf("cell[%d] height = %d, want 10", i, h)
		}
	}
	// Total output height should be 20.
	if h := lipgloss.Height(got); h != 20 {
		t.Errorf("total height = %d, want 20", h)
	}
}

func TestGrid_RowGap(t *testing.T) {
	t.Parallel()
	// 6 cells in 3 columns = 2 rows. Gap=1 affects both axes.
	// Row height: (20 - 1 gap) / 2 = 9 per row (last gets 10).
	var heights [6]int
	Grid(60, 20, 3).
		Gap(1).
		Cell(func(w, h int) string { heights[0] = h; return "" }).
		Cell(func(w, h int) string { heights[1] = h; return "" }).
		Cell(func(w, h int) string { heights[2] = h; return "" }).
		Cell(func(w, h int) string { heights[3] = h; return "" }).
		Cell(func(w, h int) string { heights[4] = h; return "" }).
		Cell(func(w, h int) string { heights[5] = h; return "" }).
		Render()

	// Available height after 1 gap between 2 rows: 20 - 1 = 19. 19/2 = 9 (last gets 10).
	if heights[0] != 9 {
		t.Errorf("row1 cell height = %d, want 9", heights[0])
	}
	if heights[3] != 10 {
		t.Errorf("row2 cell height = %d, want 10", heights[3])
	}
}

func TestGrid_OutputWidth(t *testing.T) {
	t.Parallel()
	// Total output width should fill the available width.
	got := Grid(60, 10, 3).
		Cell(func(w, h int) string { return "A" }).
		Cell(func(w, h int) string { return "B" }).
		Cell(func(w, h int) string { return "C" }).
		Render()

	if w := lipgloss.Width(got); w != 60 {
		t.Errorf("total width = %d, want 60", w)
	}
}

func TestGrid_ZeroDimensions(t *testing.T) {
	t.Parallel()
	got := Grid(0, 0, 3).
		Cell(func(w, h int) string { return "hello" }).
		Render()
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGrid_NoCells(t *testing.T) {
	t.Parallel()
	got := Grid(60, 10, 3).Render()
	if got != "" {
		t.Errorf("expected empty string for no cells, got %q", got)
	}
}

func TestGrid_SingleColumn(t *testing.T) {
	t.Parallel()
	// 3 cells in 1 column → 3 rows.
	var heights [3]int
	Grid(40, 30, 1).
		Cell(func(w, h int) string { heights[0] = h; return "" }).
		Cell(func(w, h int) string { heights[1] = h; return "" }).
		Cell(func(w, h int) string { heights[2] = h; return "" }).
		Render()

	// 30 / 3 = 10 per row.
	for i, h := range heights {
		if h != 10 {
			t.Errorf("cell[%d] height = %d, want 10", i, h)
		}
	}
}

func TestGrid_CellsSlice(t *testing.T) {
	t.Parallel()
	// Test the Cells helper that adds multiple cells from a slice.
	items := []string{"A", "B", "C", "D"}
	var count int
	Grid(80, 20, 2).
		Cells(len(items), func(i, w, h int) string {
			count++
			return items[i]
		}).
		Render()

	if count != 4 {
		t.Errorf("cell callback called %d times, want 4", count)
	}
}

func TestGrid_OutputContent(t *testing.T) {
	t.Parallel()
	// Verify cells are actually rendered in output.
	got := Grid(60, 5, 3).
		Cell(func(w, h int) string { return fmt.Sprintf("<%d>", w) }).
		Cell(func(w, h int) string { return "MID" }).
		Cell(func(w, h int) string { return "END" }).
		Render()

	if got == "" {
		t.Error("expected non-empty output")
	}
}
