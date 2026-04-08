package app

// cursor.go — Cursor navigation helpers: row movement (up, down, half-page),
// column movement (left, right, home, end), and cell selection queries.

// clampCursor clamps cursor into [0, total-1]. Returns 0 when total <= 0.
func clampCursor(cursor, total int) int {
	if total <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= total {
		return total - 1
	}
	return cursor
}

func (m *Model) cursorDown(tab *Tab) {
	total := len(tab.CellRows)
	if total == 0 {
		return
	}
	cursor := tab.Table.Cursor()
	if cursor < total-1 {
		tab.Table.SetCursor(cursor + 1)
	}
}

func (m *Model) cursorUp(tab *Tab) {
	cursor := tab.Table.Cursor()
	if cursor > 0 {
		tab.Table.SetCursor(cursor - 1)
	}
}

func (m *Model) halfPageDown(tab *Tab) {
	total := len(tab.CellRows)
	if total == 0 {
		return
	}
	half := tab.Table.Height() / 2
	if half < 1 {
		half = 1
	}
	next := tab.Table.Cursor() + half
	if next >= total {
		next = total - 1
	}
	tab.Table.SetCursor(next)
}

func (m *Model) halfPageUp(tab *Tab) {
	half := tab.Table.Height() / 2
	if half < 1 {
		half = 1
	}
	next := tab.Table.Cursor() - half
	if next < 0 {
		next = 0
	}
	tab.Table.SetCursor(next)
}

func (m *Model) colLeft(tab *Tab) {
	// Find the previous visible, selectable column.
	for i := tab.ColCursor - 1; i >= 0; i-- {
		if tab.Specs[i].HideOrder == 0 && tab.Specs[i].Kind != cellReadonly {
			tab.ColCursor = i
			m.updateTabViewport(tab)
			return
		}
	}
}

func (m *Model) colRight(tab *Tab) {
	for i := tab.ColCursor + 1; i < len(tab.Specs); i++ {
		if tab.Specs[i].HideOrder == 0 && tab.Specs[i].Kind != cellReadonly {
			tab.ColCursor = i
			m.updateTabViewport(tab)
			return
		}
	}
}

// hasSelectableCol returns true if the tab has at least one visible, non-readonly column.
func hasSelectableCol(tab *Tab) bool {
	for _, s := range tab.Specs {
		if s.HideOrder == 0 && s.Kind != cellReadonly {
			return true
		}
	}
	return false
}

func (m *Model) visibleColCount(tab *Tab) int {
	count := 0
	for _, s := range tab.Specs {
		if s.HideOrder == 0 {
			count++
		}
	}
	return count
}

func (m *Model) firstSelectableCol(tab *Tab) int {
	for i, s := range tab.Specs {
		if s.HideOrder == 0 && s.Kind != cellReadonly {
			return i
		}
	}
	return 0
}

func (m *Model) lastSelectableCol(tab *Tab) int {
	for i := len(tab.Specs) - 1; i >= 0; i-- {
		if tab.Specs[i].HideOrder == 0 && tab.Specs[i].Kind != cellReadonly {
			return i
		}
	}
	return 0
}

func (m *Model) selectedCell(tab *Tab) *cell {
	cursor := tab.Table.Cursor()
	if cursor < 0 || cursor >= len(tab.CellRows) {
		return nil
	}
	col := tab.ColCursor
	if col < 0 || col >= len(tab.CellRows[cursor]) {
		return nil
	}
	return &tab.CellRows[cursor][col]
}
