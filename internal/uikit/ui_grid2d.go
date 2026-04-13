package uikit

// Grid2D is a 2-D cursor for grid-like layouts (kanban columns × cards,
// table columns × rows, etc.). Col is the horizontal axis, Row the
// vertical axis within the current column.
//
// A Row of -1 means "column selected, no row highlighted" — useful for
// grids where entering a column and selecting a cell are distinct steps.
type Grid2D struct {
	Col int
	Row int
}

// Move shifts the cursor by (dc, dr) within a grid of the given
// dimensions. cols is the total number of columns; rowsIn returns the
// row count for a given column index.
//
// Horizontal movement clamps at [0, cols-1]. When moving to a new column,
// Row is clamped to [0, rows-1] of the destination column (or set to -1
// if the column is empty).
//
// Vertical movement wraps: going past the last row returns to 0, and
// going before 0 wraps to the last row. Moving down from Row == -1
// enters at Row 0.
func (g *Grid2D) Move(dc, dr int, cols int, rowsIn func(col int) int) {
	if cols <= 0 {
		return
	}

	// Horizontal: clamp.
	if dc != 0 {
		g.Col = clamp(g.Col+dc, 0, cols-1)
		rows := rowsIn(g.Col)
		switch {
		case rows == 0:
			g.Row = -1
		case g.Row >= rows:
			g.Row = rows - 1
		}
	}

	// Vertical: wrap within the current column.
	if dr != 0 {
		rows := rowsIn(g.Col)
		if rows == 0 {
			g.Row = -1
			return
		}
		if g.Row < 0 {
			// Entering from -1: down → first, up → last.
			if dr > 0 {
				g.Row = 0
			} else {
				g.Row = rows - 1
			}
			return
		}
		g.Row = (g.Row + dr%rows + rows) % rows
	}
}

// Clamp adjusts Col and Row so they fall within the current grid
// dimensions without moving the cursor intentionally. Equivalent to
// Move(0, 0, ...) but reads more clearly at the call site.
func (g *Grid2D) Clamp(cols int, rowsIn func(col int) int) {
	if cols <= 0 {
		g.Col = 0
		g.Row = -1
		return
	}
	g.Col = clamp(g.Col, 0, cols-1)
	rows := rowsIn(g.Col)
	switch {
	case rows == 0:
		g.Row = -1
	case g.Row >= rows:
		g.Row = rows - 1
	}
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
