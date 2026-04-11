package app

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	engine "github.com/sciminds/cli/internal/board"
	"github.com/sciminds/cli/internal/tui/board/ui"
)

func sortCardsByPosition(cards []engine.Card) {
	sort.SliceStable(cards, func(i, j int) bool {
		return cards[i].Position < cards[j].Position
	})
}

// viewGrid renders the kanban grid: columns side-by-side, each a bordered
// frame containing its cards. Focused column / card are highlighted.
func (m *Model) viewGrid(width, height int) string {
	if len(m.current.Columns) == 0 {
		return m.styles.Help.Render("  empty board — no columns yet")
	}

	// Compute per-column width: distribute available width evenly,
	// clamped to ColumnWidth * 1.3 max so they don't stretch absurdly.
	nCols := len(m.current.Columns)
	gapsTotal := ui.ColumnGap * (nCols - 1)
	avail := width - gapsTotal
	if avail < nCols*8 {
		avail = nCols * 8
	}
	colW := avail / nCols
	maxW := ui.ColumnWidth + ui.ColumnWidth/3
	if colW > maxW {
		colW = maxW
	}
	if colW < 10 {
		colW = 10
	}

	// Interior height for each column: subtract 2 for frame top/bottom borders.
	interiorH := height - 2
	if interiorH < 3 {
		interiorH = 3
	}

	byCol := m.cardsByColumn()
	rendered := make([]string, nCols)
	for i, col := range m.current.Columns {
		rendered[i] = m.renderColumn(col, byCol[col.ID], i, colW, interiorH)
	}

	gap := strings.Repeat(" ", ui.ColumnGap)
	return lipgloss.JoinHorizontal(lipgloss.Top, joinWithGap(rendered, gap)...)
}

// joinWithGap interleaves a spacer between each rendered column.
func joinWithGap(parts []string, gap string) []string {
	if len(parts) <= 1 {
		return parts
	}
	out := make([]string, 0, len(parts)*2-1)
	for i, p := range parts {
		if i > 0 {
			out = append(out, gap)
		}
		out = append(out, p)
	}
	return out
}

func (m *Model) renderColumn(col engine.Column, cards []engine.Card, colIdx, width, height int) string {
	focus := colIdx == m.cur.col
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

	for i, c := range cards {
		sel := focus && i == m.cur.card
		body = append(body, m.renderCard(c, innerW, sel))
		if i < len(cards)-1 {
			body = append(body, strings.Repeat("", ui.CardGap)) // empty line as gap
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

	return frame.Width(width - 2).Render(strings.Join(body, "\n"))
}

func (m *Model) renderColumnHeader(col engine.Column, count, innerW int) string {
	title := truncate(col.Title, innerW-6)
	countStr := fmt.Sprintf(" %d", count)
	if col.WIP > 0 {
		countStr = fmt.Sprintf(" %d/%d", count, col.WIP)
	}
	return m.styles.ColumnTitle.Render(title) + m.styles.ColumnCount.Render(countStr)
}

func (m *Model) renderCard(c engine.Card, width int, selected bool) string {
	titleStyle := m.styles.CardTitle
	wrap := m.styles.Card
	if selected {
		titleStyle = m.styles.CardSelected
		wrap = m.styles.CardSelected
	}
	title := truncate(c.Title, width-2)
	parts := []string{titleStyle.Render(title)}

	var metaBits []string
	if c.Priority != "" {
		metaBits = append(metaBits, m.styles.CardPriority.Render("!"+c.Priority))
	}
	if len(c.Labels) > 0 {
		metaBits = append(metaBits, m.styles.CardLabel.Render(strings.Join(c.Labels, " ")))
	}
	if len(metaBits) > 0 {
		parts = append(parts, truncate(strings.Join(metaBits, " "), width-2))
	}

	return wrap.Width(width).Render(strings.Join(parts, "\n"))
}
