package kit

import "testing"

// rows returns a rowsIn function that maps column index → row count
// from a fixed slice.
func rows(counts ...int) func(int) int {
	return func(col int) int {
		if col < 0 || col >= len(counts) {
			return 0
		}
		return counts[col]
	}
}

// ── Move: horizontal ───────────────────────────────────────────────────

func TestMoveRight(t *testing.T) {
	g := Grid2D{Col: 0, Row: 1}
	g.Move(1, 0, 3, rows(2, 3, 1))
	if g.Col != 1 || g.Row != 1 {
		t.Errorf("got col=%d row=%d, want col=1 row=1", g.Col, g.Row)
	}
}

func TestMoveRightClampsAtEnd(t *testing.T) {
	g := Grid2D{Col: 2, Row: 0}
	g.Move(1, 0, 3, rows(2, 3, 1))
	if g.Col != 2 {
		t.Errorf("col=%d, want 2 (clamped)", g.Col)
	}
}

func TestMoveLeftClampsAtZero(t *testing.T) {
	g := Grid2D{Col: 0, Row: 0}
	g.Move(-1, 0, 3, rows(2, 3, 1))
	if g.Col != 0 {
		t.Errorf("col=%d, want 0 (clamped)", g.Col)
	}
}

func TestMoveRightClampsRowToNewColumn(t *testing.T) {
	g := Grid2D{Col: 0, Row: 2}
	g.Move(1, 0, 2, rows(5, 1)) // destination has only 1 row
	if g.Col != 1 || g.Row != 0 {
		t.Errorf("got col=%d row=%d, want col=1 row=0 (clamped)", g.Col, g.Row)
	}
}

func TestMoveRightIntoEmptyColumn(t *testing.T) {
	g := Grid2D{Col: 0, Row: 1}
	g.Move(1, 0, 2, rows(3, 0)) // destination is empty
	if g.Col != 1 || g.Row != -1 {
		t.Errorf("got col=%d row=%d, want col=1 row=-1", g.Col, g.Row)
	}
}

// ── Move: vertical ─────────────────────────────────────────────────────

func TestMoveDownWraps(t *testing.T) {
	g := Grid2D{Col: 0, Row: 1}
	g.Move(0, 1, 1, rows(2)) // last row → wraps to 0
	if g.Row != 0 {
		t.Errorf("row=%d, want 0 (wrapped)", g.Row)
	}
}

func TestMoveUpWraps(t *testing.T) {
	g := Grid2D{Col: 0, Row: 0}
	g.Move(0, -1, 1, rows(3)) // first row → wraps to 2
	if g.Row != 2 {
		t.Errorf("row=%d, want 2 (wrapped)", g.Row)
	}
}

func TestMoveDownFromUnfocused(t *testing.T) {
	g := Grid2D{Col: 0, Row: -1}
	g.Move(0, 1, 1, rows(3))
	if g.Row != 0 {
		t.Errorf("row=%d, want 0 (enter from -1 going down)", g.Row)
	}
}

func TestMoveUpFromUnfocused(t *testing.T) {
	g := Grid2D{Col: 0, Row: -1}
	g.Move(0, -1, 1, rows(3))
	if g.Row != 2 {
		t.Errorf("row=%d, want 2 (enter from -1 going up)", g.Row)
	}
}

func TestMoveVerticalEmptyColumn(t *testing.T) {
	g := Grid2D{Col: 0, Row: -1}
	g.Move(0, 1, 1, rows(0))
	if g.Row != -1 {
		t.Errorf("row=%d, want -1 (empty column)", g.Row)
	}
}

// ── Move: combined ─────────────────────────────────────────────────────

func TestMoveDiagonalIgnored(t *testing.T) {
	// dc and dr both nonzero: horizontal applied first, then vertical.
	g := Grid2D{Col: 0, Row: 0}
	g.Move(1, 1, 2, rows(3, 3))
	if g.Col != 1 || g.Row != 1 {
		t.Errorf("got col=%d row=%d, want col=1 row=1", g.Col, g.Row)
	}
}

// ── Move: zero cols ────────────────────────────────────────────────────

func TestMoveZeroCols(t *testing.T) {
	g := Grid2D{Col: 0, Row: 0}
	g.Move(1, 0, 0, rows()) // should be a no-op
	if g.Col != 0 || g.Row != 0 {
		t.Errorf("got col=%d row=%d, want no change", g.Col, g.Row)
	}
}

// ── Clamp ──────────────────────────────────────────────────────────────

func TestClampWithinBounds(t *testing.T) {
	g := Grid2D{Col: 1, Row: 2}
	g.Clamp(3, rows(5, 5, 5))
	if g.Col != 1 || g.Row != 2 {
		t.Errorf("got col=%d row=%d, want no change", g.Col, g.Row)
	}
}

func TestClampColOverflow(t *testing.T) {
	g := Grid2D{Col: 5, Row: 0}
	g.Clamp(3, rows(2, 2, 2))
	if g.Col != 2 {
		t.Errorf("col=%d, want 2 (clamped)", g.Col)
	}
}

func TestClampRowOverflow(t *testing.T) {
	g := Grid2D{Col: 0, Row: 10}
	g.Clamp(2, rows(3, 3))
	if g.Row != 2 {
		t.Errorf("row=%d, want 2 (clamped)", g.Row)
	}
}

func TestClampEmptyGrid(t *testing.T) {
	g := Grid2D{Col: 5, Row: 5}
	g.Clamp(0, rows())
	if g.Col != 0 || g.Row != -1 {
		t.Errorf("got col=%d row=%d, want col=0 row=-1", g.Col, g.Row)
	}
}

func TestClampEmptyColumn(t *testing.T) {
	g := Grid2D{Col: 1, Row: 3}
	g.Clamp(2, rows(5, 0))
	if g.Row != -1 {
		t.Errorf("row=%d, want -1 (empty column)", g.Row)
	}
}
