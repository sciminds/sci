// layout_grid.go — auto-flow N-column grid layout.
//
// Grid distributes cells into equal-width columns across rows, like CSS
// Grid's `repeat(N, 1fr)`. Each cell callback receives its allocated
// dimensions, eliminating manual column-width arithmetic.
//
//	// 3-column grid with gap spacing
//	uikit.Grid(width, height, 3).
//	    Gap(1).
//	    Cell(func(w, h int) string { return renderCard(card1, w, h) }).
//	    Cell(func(w, h int) string { return renderCard(card2, w, h) }).
//	    Cell(func(w, h int) string { return renderCard(card3, w, h) }).
//	    Render()
//
//	// Iterate a slice of items
//	uikit.Grid(width, height, 3).
//	    Cells(len(items), func(i, w, h int) string {
//	        return renderItem(items[i], w, h)
//	    }).
//	    Render()

package uikit

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// gridCell is one entry in a GridLayout's cell list.
type gridCell struct {
	fn func(w, h int) string
}

// GridLayout is a builder for N-column auto-flow grid layouts.
// Use [Grid] to create one.
type GridLayout struct {
	width   int
	height  int
	columns int
	gap     int
	cells   []gridCell
}

// Grid creates an N-column grid layout builder. Cells flow left-to-right,
// top-to-bottom into rows of equal-width columns.
func Grid(width, height, columns int) *GridLayout {
	if columns < 1 {
		columns = 1
	}
	return &GridLayout{
		width:   width,
		height:  height,
		columns: columns,
	}
}

// Gap sets the spacing between cells (both horizontal and vertical).
func (g *GridLayout) Gap(n int) *GridLayout {
	if n < 0 {
		n = 0
	}
	g.gap = n
	return g
}

// Cell adds a single cell to the grid. The callback receives the cell's
// allocated width and height.
func (g *GridLayout) Cell(fn func(w, h int) string) *GridLayout {
	g.cells = append(g.cells, gridCell{fn: fn})
	return g
}

// Cells adds n cells using an indexed callback. This is the idiomatic way
// to populate a grid from a slice:
//
//	grid.Cells(len(items), func(i, w, h int) string {
//	    return renderItem(items[i], w, h)
//	})
func (g *GridLayout) Cells(n int, fn func(index, w, h int) string) *GridLayout {
	for i := range n {
		i := i // capture
		g.cells = append(g.cells, gridCell{fn: func(w, h int) string {
			return fn(i, w, h)
		}})
	}
	return g
}

// Render computes the grid layout and returns the composed string.
func (g *GridLayout) Render() string {
	if g.width <= 0 || g.height <= 0 || len(g.cells) == 0 {
		return ""
	}

	// Compute cell dimensions.
	rowCount := (len(g.cells) + g.columns - 1) / g.columns
	totalHGap := g.gap * (g.columns - 1)
	totalVGap := g.gap * (rowCount - 1)

	availW := g.width - totalHGap
	availH := g.height - totalVGap

	colW := availW / g.columns
	if colW < 1 {
		colW = 1
	}

	rowH := availH / rowCount
	if rowH < 1 {
		rowH = 1
	}

	// Render rows.
	rows := make([]string, 0, rowCount)
	cellIdx := 0

	for row := range rowCount {
		// Last row gets remaining height (avoids rounding loss).
		thisRowH := rowH
		if row == rowCount-1 {
			thisRowH = availH - rowH*(rowCount-1)
			if thisRowH < 1 {
				thisRowH = 1
			}
		}

		rowCells := make([]string, 0, g.columns)
		for col := range g.columns {
			if cellIdx >= len(g.cells) {
				// Pad with empty cells for alignment.
				thisColW := colW
				if col == g.columns-1 {
					thisColW = availW - colW*(g.columns-1)
				}
				rowCells = append(rowCells, lipgloss.NewStyle().
					Width(thisColW).
					Height(thisRowH).
					Render(""))
				continue
			}

			// Last column in row gets remaining width.
			thisColW := colW
			if col == g.columns-1 {
				thisColW = availW - colW*(g.columns-1)
				if thisColW < 1 {
					thisColW = 1
				}
			}

			content := g.cells[cellIdx].fn(thisColW, thisRowH)
			content = lipgloss.NewStyle().
				Width(thisColW).
				Height(thisRowH).
				Render(content)
			rowCells = append(rowCells, content)
			cellIdx++
		}

		if g.gap > 0 {
			spacer := strings.Repeat(" ", g.gap)
			row := joinHWithSpacer(rowCells, spacer)
			rows = append(rows, row)
		} else {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rowCells...))
		}
	}

	if g.gap > 0 {
		spacer := strings.Repeat("\n", g.gap)
		return joinVWithSpacer(rows, spacer)
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// joinHWithSpacer joins strings horizontally with a spacer between them.
func joinHWithSpacer(items []string, spacer string) string {
	if len(items) == 0 {
		return ""
	}
	interleaved := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			interleaved = append(interleaved, spacer)
		}
		interleaved = append(interleaved, item)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, interleaved...)
}

// joinVWithSpacer joins strings vertically with a spacer between them.
func joinVWithSpacer(items []string, spacer string) string {
	if len(items) == 0 {
		return ""
	}
	interleaved := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			interleaved = append(interleaved, spacer)
		}
		interleaved = append(interleaved, item)
	}
	return lipgloss.JoinVertical(lipgloss.Left, interleaved...)
}
