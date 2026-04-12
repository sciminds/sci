package app

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
)

func sortCardsByPosition(cards []engine.Card) {
	slices.SortStableFunc(cards, func(a, b engine.Card) int {
		return cmp.Compare(a.Position, b.Position)
	})
}

// gridLayout describes which columns are visible and how wide each is.
// Computed once per render by computeGridLayout.
type gridLayout struct {
	start, end int   // visible column range [start, end)
	colWidth   []int // per-column width (len == total columns)
	windowed   bool  // true when horizontal scrolling is active
	leftArrow  bool  // render ‹ in left gutter
	rightArrow bool  // render › in right gutter
}

// computeGridLayout decides between stretch mode (all columns fit at
// MinColumnWidth) and windowed mode (fixed-width columns, horizontally
// scrolled from m.gridScroll). Collapsed columns always take
// CollapsedColumnWidth regardless of mode.
func (m *Model) computeGridLayout(width int) gridLayout {
	n := len(m.current.Columns)
	if n == 0 {
		return gridLayout{}
	}
	widths := make([]int, n)
	gaps := (n - 1) * ui.ColumnGap

	minSum := gaps
	expCount := 0
	for _, col := range m.current.Columns {
		if m.collapsed[col.ID] {
			minSum += ui.CollapsedColumnWidth
		} else {
			minSum += ui.MinColumnWidth
			expCount++
		}
	}

	if minSum <= width {
		// Stretch mode: distribute any extra width across expanded columns
		// up to ColumnWidth each.
		extra := width - minSum
		perExp := 0
		if expCount > 0 {
			perExp = extra / expCount
			if cap := ui.ColumnWidth - ui.MinColumnWidth; perExp > cap {
				perExp = cap
			}
		}
		for i, col := range m.current.Columns {
			if m.collapsed[col.ID] {
				widths[i] = ui.CollapsedColumnWidth
			} else {
				widths[i] = ui.MinColumnWidth + perExp
			}
		}
		return gridLayout{start: 0, end: n, colWidth: widths}
	}

	// Windowed mode: fixed widths, walk forward from gridScroll until
	// the budget is exhausted. Two cells reserved for arrow gutters.
	for i, col := range m.current.Columns {
		if m.collapsed[col.ID] {
			widths[i] = ui.CollapsedColumnWidth
		} else {
			widths[i] = ui.ColumnWidth
		}
	}

	start := m.gridScroll
	if start < 0 {
		start = 0
	}
	if start > n-1 {
		start = n - 1
	}

	budget := width - 2*ui.ScrollGutter
	if budget < widths[start] {
		budget = widths[start] // always render at least one column
	}

	end := start
	used := 0
	for end < n {
		add := widths[end]
		if end > start {
			add += ui.ColumnGap
		}
		if used+add > budget && end > start {
			break
		}
		used += add
		end++
	}

	return gridLayout{
		start:      start,
		end:        end,
		colWidth:   widths,
		windowed:   true,
		leftArrow:  start > 0,
		rightArrow: end < n,
	}
}

// visibleColumnRange reports the [start, end) range of visible columns at
// the given total body width.
func (m *Model) visibleColumnRange(width int) (start, end int) {
	l := m.computeGridLayout(width)
	return l.start, l.end
}

// ensureCursorVisible adjusts m.gridScroll so m.cur.Col lands inside the
// visible window at the given width. Called after any h/l navigation.
func (m *Model) ensureCursorVisible(width int) {
	n := len(m.current.Columns)
	if n == 0 || m.cur.Col < 0 || m.cur.Col >= n {
		return
	}
	start, end := m.visibleColumnRange(width)
	if m.cur.Col < start {
		m.gridScroll = m.cur.Col
		return
	}
	// Advance gridScroll one column at a time until cursor is visible.
	guard := n
	for m.cur.Col >= end && m.gridScroll < n-1 && guard > 0 {
		m.gridScroll++
		_, end = m.visibleColumnRange(width)
		guard--
	}
}

// toggleCollapseCurrent toggles collapse state for the column under the
// cursor. View-only — not persisted to the event log.
func (m *Model) toggleCollapseCurrent() {
	if m.cur.Col < 0 || m.cur.Col >= len(m.current.Columns) {
		return
	}
	id := m.current.Columns[m.cur.Col].ID
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	if m.collapsed[id] {
		delete(m.collapsed, id)
	} else {
		m.collapsed[id] = true
	}
}

// expandAll clears the collapse map so every column renders in full.
func (m *Model) expandAll() {
	m.collapsed = map[string]bool{}
}

// viewGrid renders the kanban grid: columns side-by-side, each a bordered
// frame containing its cards. Focused column / card are highlighted.
// Supports horizontal scrolling and per-column collapse.
func (m *Model) viewGrid(width, height int) string {
	if len(m.current.Columns) == 0 {
		return m.styles.Help.Render("  empty board — no columns yet")
	}

	layout := m.computeGridLayout(width)

	// Interior height for each column: subtract 2 for frame top/bottom borders.
	interiorH := height - 2
	if interiorH < 3 {
		interiorH = 3
	}

	byCol := m.cardsByColumn()

	var parts []string
	if layout.windowed {
		parts = append(parts, m.renderScrollGutter(layout.leftArrow, "‹", interiorH))
	}

	for i := layout.start; i < layout.end; i++ {
		col := m.current.Columns[i]
		w := layout.colWidth[i]
		var rendered string
		if m.collapsed[col.ID] {
			rendered = m.renderCollapsedColumn(col, len(byCol[col.ID]), i, w, interiorH)
		} else {
			rendered = m.renderColumn(col, byCol[col.ID], i, w, interiorH)
		}
		parts = append(parts, rendered)
		if i < layout.end-1 {
			parts = append(parts, strings.Repeat(" ", ui.ColumnGap))
		}
	}

	if layout.windowed {
		parts = append(parts, m.renderScrollGutter(layout.rightArrow, "›", interiorH))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// renderScrollGutter produces a ScrollGutter-wide column of text. When
// `show` is true the arrow glyph appears on the middle row, otherwise the
// gutter is blank. Height matches the interior column height + 2 borders
// so lipgloss.JoinHorizontal aligns correctly.
func (m *Model) renderScrollGutter(show bool, glyph string, interiorH int) string {
	total := interiorH + 2
	rows := make([]string, total)
	for i := range rows {
		rows[i] = " "
	}
	if show {
		rows[total/2] = m.styles.ScrollIndicator.Render(glyph)
	}
	return strings.Join(rows, "\n")
}

// renderCollapsedColumn draws a narrow bordered strip showing an
// abbreviated column title and card count — the hidden-column
// counterpart to renderColumn.
func (m *Model) renderCollapsedColumn(col engine.Column, count, colIdx, width, height int) string {
	focus := colIdx == m.cur.Col
	frame := m.styles.ColumnCollapsed
	if focus {
		frame = m.styles.ColumnFocus
	}

	innerW := width - 4 // 2 border + 2 horizontal padding
	if innerW < 1 {
		innerW = 1
	}

	abbr := strings.ToUpper(col.Title)
	if r := []rune(abbr); len(r) > innerW {
		abbr = string(r[:innerW])
	}
	countStr := fmt.Sprintf("%d", count)
	if lipgloss.Width(countStr) > innerW {
		countStr = truncate(countStr, innerW)
	}

	body := []string{
		m.styles.ColumnTitle.Render(centerPad(abbr, innerW)),
		"",
		m.styles.ColumnCount.Render(centerPad(countStr, innerW)),
	}
	for len(body) < height {
		body = append(body, "")
	}
	if len(body) > height {
		body = body[:height]
	}

	return frame.Width(width).Render(strings.Join(body, "\n"))
}

// centerPad pads s with spaces so its visible width equals w, centered.
func centerPad(s string, w int) string {
	diff := w - lipgloss.Width(s)
	if diff <= 0 {
		return s
	}
	left := diff / 2
	right := diff - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func (m *Model) renderColumn(col engine.Column, cards []engine.Card, colIdx, width, height int) string {
	focus := colIdx == m.cur.Col
	frame := m.styles.ColumnFrame
	if focus {
		frame = m.styles.ColumnFocus
	}

	// Content width = frame width - 2 borders - 2 inner padding
	innerW := width - 4
	if innerW < 6 {
		innerW = 6
	}

	header := m.renderColumnHeader(col, len(cards), innerW)
	var body []string
	body = append(body, header)
	body = append(body, "")

	// Card outer width = interior width (each card carries its own border).
	cardW := innerW
	if cardW < 6 {
		cardW = 6
	}

	for i, c := range cards {
		sel := focus && i == m.cur.Row
		body = append(body, strings.Split(m.renderCard(c, cardW, sel), "\n")...)
		if i < len(cards)-1 {
			for g := 0; g < ui.CardGap; g++ {
				body = append(body, "")
			}
		}
	}

	// Clip to height (interior) — leave room for header + blank line already.
	if len(body) > height {
		body = body[:height]
	}
	for len(body) < height {
		body = append(body, "")
	}

	// frame.Width sets the *total* rendered width (border + padding + content),
	// so pass width directly. Subtracting here would shrink the content area
	// below innerW and wrap the header onto a second row, pushing the bottom
	// border off the bottom of the screen.
	return frame.Width(width).Render(strings.Join(body, "\n"))
}

// renderColumnHeader renders a single-line column header: uppercased,
// underlined accent title on the left and a dim count (or count/WIP) on
// the right. Plain text — no background fill.
func (m *Model) renderColumnHeader(col engine.Column, count, innerW int) string {
	countStr := fmt.Sprintf("%d", count)
	if col.WIP > 0 {
		countStr = fmt.Sprintf("%d/%d", count, col.WIP)
	}

	// Reserve room for the count plus a one-cell gap before it.
	countW := lipgloss.Width(countStr)
	titleAvail := innerW - countW - 1
	if titleAvail < 1 {
		titleAvail = 1
	}
	title := truncate(strings.ToUpper(col.Title), titleAvail)

	// Right-align the count by padding the gap between title and count.
	gap := innerW - lipgloss.Width(title) - countW
	if gap < 1 {
		gap = 1
	}

	return m.styles.ColumnTitle.Render(title) +
		strings.Repeat(" ", gap) +
		m.styles.ColumnCount.Render(countStr)
}

func (m *Model) renderCard(c engine.Card, width int, selected bool) string {
	wrap := m.styles.Card
	if selected {
		wrap = m.styles.CardSelected
	}
	// Inner content width: subtract 2 borders + 2 horizontal padding cells.
	contentW := width - 4
	if contentW < 1 {
		contentW = 1
	}

	title := truncate(c.Title, contentW)
	parts := []string{m.styles.CardTitle.Render(title)}

	var metaBits []string
	if c.Priority != "" {
		metaBits = append(metaBits, m.styles.CardPriority.Render("!"+c.Priority))
	}
	if len(c.Labels) > 0 {
		metaBits = append(metaBits, m.styles.CardLabel.Render(strings.Join(c.Labels, " ")))
	}
	if len(metaBits) > 0 {
		parts = append(parts, truncate(strings.Join(metaBits, " "), contentW))
	}

	return wrap.Width(contentW).Render(strings.Join(parts, "\n"))
}
