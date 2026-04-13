package app

// table.go — table rendering: column layout, cell truncation, separator
// drawing, and filter/sort indicator glyphs in column headers.

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/compose"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

const (
	filterMarkActive          = "\u25bc" // ▼
	filterMarkActiveInverted  = "\u25b2" // ▲
	filterMarkPreview         = "\u25bd" // ▽
	filterMarkPreviewInverted = "\u25b3" // △
)

// visibleProjection computes the visible-only view of a tab's columns and data.
func visibleProjection(tab *Tab) (
	specs []columnSpec,
	cellRows [][]cell,
	colCursor int,
	sorts []sortEntry,
	visToFull []int,
) {
	fullToVis := make(map[int]int, len(tab.Specs))
	for i, spec := range tab.Specs {
		if spec.HideOrder > 0 {
			continue
		}
		fullToVis[i] = len(visToFull)
		visToFull = append(visToFull, i)
		specs = append(specs, spec)
	}

	colCursor = -1
	if vis, ok := fullToVis[tab.ColCursor]; ok {
		colCursor = vis
	}

	cellRows = lo.Map(tab.CellRows, func(row []cell, _ int) []cell {
		return lo.FilterMap(visToFull, func(fi int, _ int) (cell, bool) {
			if fi < len(row) {
				return row[fi], true
			}
			return cell{}, false
		})
	})

	for _, se := range tab.Sorts {
		if vis, ok := fullToVis[se.Col]; ok {
			sorts = append(sorts, sortEntry{Col: vis, Dir: se.Dir})
		}
	}
	return
}

func renderHeaderRow(
	specs []columnSpec,
	widths []int,
	separators []string,
	colCursor int,
	sorts []sortEntry,
	hasLeft, hasRight bool,
	zones *zone.Manager,
	colZonePrefix string,
) string {
	cells := make([]string, 0, len(specs))
	last := len(specs) - 1
	arrow := ui.TUI.SortArrow()
	for i, spec := range specs {
		width := safeWidth(widths, i)
		title := spec.Title
		if i == 0 && hasLeft {
			title = arrow.Render("\u25c0") + " " + title
		}
		if i == last && hasRight {
			title = title + " " + arrow.Render("\u25b6")
		}
		indicator := sortIndicator(sorts, i)
		text := formatHeaderCell(title, indicator, width)
		var rendered string
		if i == colCursor {
			rendered = ui.TUI.ColActiveHeader().Render(text)
		} else {
			rendered = ui.TUI.TableHeader().Render(text)
		}
		cells = append(cells, zones.Mark(fmt.Sprintf("%s%d", colZonePrefix, i), rendered))
	}
	return joinCells(cells, separators)
}

type tableViewport struct {
	HasLeft       bool
	HasRight      bool
	Specs         []columnSpec
	Cells         [][]cell
	Widths        []int
	PlainSeps     []string
	CollapsedSeps []string
	Cursor        int
	Sorts         []sortEntry
	VisToFull     []int
}

func computeTableViewport(
	tab *Tab,
	termWidth int,
	normalSep string,
) tableViewport {
	var vp tableViewport
	if tab == nil {
		return vp
	}
	visSpecs, visCells, visColCursor, visSorts, visToFull := visibleProjection(tab)
	if len(visSpecs) == 0 {
		return vp
	}

	hasPins := len(tab.Pins) > 0 && len(tab.FullCellRows) > 0
	var visNatural []int
	if hasPins {
		visNatural = naturalWidthsIndirect(visSpecs, tab.FullCellRows, visToFull)
	} else {
		visNatural = naturalWidths(visSpecs, visCells)
	}

	sepW := lipgloss.Width(normalSep)
	fullWidths := columnWidths(visSpecs, visCells, termWidth, sepW, visNatural)

	start, end, hasLeft, hasRight := viewportRange(
		fullWidths, sepW, termWidth, tab.ViewOffset, visColCursor,
	)
	vp.HasLeft = hasLeft
	vp.HasRight = hasRight

	vp.Specs = sliceViewport(visSpecs, start, end)
	vp.Cells = sliceViewportRows(visCells, start, end)
	vp.Sorts = viewportSorts(visSorts, start)
	vpVisToFull := sliceViewport(visToFull, start, end)

	vp.Cursor = visColCursor - start
	if visColCursor < start || visColCursor >= end {
		vp.Cursor = -1
	}
	vp.VisToFull = vpVisToFull

	fullCells := vp.Cells
	if hasPins {
		fullCells = projectCellRows(tab.FullCellRows, visToFull, start, end)
	}
	vp.Widths = columnWidths(vp.Specs, fullCells, termWidth, sepW, visNatural[start:end])
	vp.PlainSeps, vp.CollapsedSeps = gapSeparators(vpVisToFull, len(tab.Specs), normalSep)

	return vp
}

func formatHeaderCell(title, indicator string, width int) string {
	if indicator == "" {
		return formatCell(title, width, alignLeft)
	}
	return compose.SpreadMinGap(width, 1, title, indicator)
}

func projectCellRows(
	fullCellRows [][]cell,
	visToFull []int,
	start, end int,
) [][]cell {
	vpMap := visToFull[start:end]
	return lo.Map(fullCellRows, func(row []cell, _ int) []cell {
		return lo.FilterMap(vpMap, func(fi int, _ int) (cell, bool) {
			if fi < len(row) {
				return row[fi], true
			}
			return cell{}, false
		})
	})
}

func viewportSorts(sorts []sortEntry, vpStart int) []sortEntry {
	if vpStart == 0 {
		return sorts
	}
	adjusted := make([]sortEntry, 0, len(sorts))
	for _, s := range sorts {
		adjusted = append(adjusted, sortEntry{Col: s.Col - vpStart, Dir: s.Dir})
	}
	return adjusted
}

func decimalDigits(n int) int {
	if n <= 0 {
		return 1
	}
	return int(math.Log10(float64(n))) + 1
}

func sortIndicatorWidth(columnCount int) int {
	if columnCount <= 1 {
		return 2
	}
	return 2 + decimalDigits(columnCount)
}

func headerTitleWidth(spec columnSpec, columnCount int) int {
	w := lipgloss.Width(spec.Title)
	w += sortIndicatorWidth(columnCount)
	return w
}

func sortIndicator(sorts []sortEntry, col int) string {
	for i, entry := range sorts {
		if entry.Col == col {
			arrow := " " + symTriUp
			if entry.Dir == sortDesc {
				arrow = " " + symTriDown
			}
			if len(sorts) == 1 {
				return arrow
			}
			return fmt.Sprintf("%s%d", arrow, i+1)
		}
	}
	return ""
}

func renderDivider(
	widths []int,
	separators []string,
	divSep string,
	style lipgloss.Style,
) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		if width < 1 {
			width = 1
		}
		parts = append(parts, style.Render(strings.Repeat("\u2500", width)))
	}
	if len(separators) > 0 {
		uniform := make([]string, len(separators))
		for i := range uniform {
			uniform[i] = divSep
		}
		separators = uniform
	}
	return joinCells(parts, separators)
}

type pinRenderContext struct {
	Pins     []filterPin
	RawCells [][]cell
	Inverted bool
}

func renderRows(
	specs []columnSpec,
	rows [][]cell,
	meta []rowMeta,
	widths []int,
	plainSeps []string,
	collapsedSeps []string,
	cursor int,
	colCursor int,
	editing bool,
	height int,
	pinCtx pinRenderContext,
	zones *zone.Manager,
	rowZonePrefix string,
	visualSel map[int]bool,
	searchHL map[int]map[int][]int,
) []string {
	total := len(rows)
	if total == 0 {
		return nil
	}
	if height <= 0 {
		height = total
	}
	start, end := visibleRange(total, height, cursor)
	count := end - start
	mid := start + count/2
	rendered := make([]string, 0, count)
	for i := start; i < end; i++ {
		selected := i == cursor
		dimmed := i < len(meta) && meta[i].Dimmed
		seps := plainSeps
		if i == start || i == mid || i == end-1 {
			seps = collapsedSeps
		}
		row := renderRow(
			specs, rows[i], widths, seps, selected, dimmed,
			colCursor, editing, pinCtx, i, visualSel, searchHL[i],
		)
		rendered = append(rendered, zones.Mark(fmt.Sprintf("%s%d", rowZonePrefix, i), row))
	}
	return rendered
}

type cellHighlight int

const (
	highlightNone cellHighlight = iota
	highlightRow
	highlightNormalCursor // active cell in normal mode (accent-tinted bg + bold)
	highlightEditCursor   // active cell in edit mode (underline + bold + warm bg)
	highlightVisual       // selected row in visual mode (not cursor)
	highlightVisualCursor // cursor row in visual mode
)

func renderRow(
	specs []columnSpec,
	row []cell,
	widths []int,
	separators []string,
	selected bool,
	dimmed bool,
	colCursor int,
	editing bool,
	pinCtx pinRenderContext,
	rowIdx int,
	visualSel map[int]bool,
	cellSearchHL map[int][]int,
) string {
	isVisual := len(visualSel) > 0
	isVisualSelected := visualSel[rowIdx]

	cells := make([]string, 0, len(specs))
	for i, spec := range specs {
		width := safeWidth(widths, i)
		var cellValue cell
		if i < len(row) {
			cellValue = row[i]
		}
		hl := highlightNone
		if isVisual {
			// In visual mode: full-row highlight, no cell-level cursor.
			if selected {
				hl = highlightVisualCursor
			} else if isVisualSelected {
				hl = highlightVisual
			}
		} else if selected && i == colCursor {
			if editing {
				hl = highlightEditCursor
			} else {
				hl = highlightNormalCursor
			}
		} else if selected {
			hl = highlightRow
		}
		pinMatch := false
		if len(pinCtx.Pins) > 0 {
			rawCell := cellValue
			if rowIdx < len(pinCtx.RawCells) && i < len(pinCtx.RawCells[rowIdx]) {
				rawCell = pinCtx.RawCells[rowIdx][i]
			}
			pinMatch = cellMatchesPin(pinCtx.Pins, i, rawCell)
			if pinCtx.Inverted && columnHasPin(pinCtx.Pins, i) {
				pinMatch = !pinMatch
			}
		}
		rendered := renderCell(cellValue, spec, width, hl, dimmed, pinMatch, cellSearchHL[i])
		cells = append(cells, rendered)
	}
	return joinCells(cells, separators)
}

// projectSearchHighlights maps search highlights from full column indices to
// viewport column indices.
func projectSearchHighlights(
	highlights map[int]map[int][]int,
	visToFull []int,
) map[int]map[int][]int {
	if len(highlights) == 0 {
		return nil
	}
	// Build reverse map: full col → viewport col.
	fullToVP := make(map[int]int, len(visToFull))
	for vpIdx, fullIdx := range visToFull {
		fullToVP[fullIdx] = vpIdx
	}
	projected := make(map[int]map[int][]int)
	for rowIdx, colMap := range highlights {
		pRow := make(map[int][]int)
		for fullCol, positions := range colMap {
			if vpCol, ok := fullToVP[fullCol]; ok {
				pRow[vpCol] = positions
			}
		}
		if len(pRow) > 0 {
			projected[rowIdx] = pRow
		}
	}
	return projected
}

func columnHasPin(pins []filterPin, col int) bool {
	for _, pin := range pins {
		if pin.Col == col {
			return true
		}
	}
	return false
}

func cellMatchesPin(pins []filterPin, col int, c cell) bool {
	key := tabstate.CellDisplayValue(c)
	for _, pin := range pins {
		if pin.Col == col {
			return pin.Values[key]
		}
	}
	return false
}

func renderCell(
	cellValue cell,
	spec columnSpec,
	width int,
	hl cellHighlight,
	dimmed bool,
	pinMatch bool,
	searchPositions []int,
) string {
	if width < 1 {
		width = 1
	}
	value := firstLine(cellValue.Value)
	style := cellStyle(cellValue.Kind)
	if cellValue.Null {
		value = symEmptySet
		style = ui.TUI.Null()
	} else if value == "" {
		value = symEmDash
		style = ui.TUI.Empty()
	}

	if pinMatch {
		style = ui.TUI.Pinned()
	}
	if dimmed {
		style = style.Foreground(ui.TUI.Palette().TextDim)
	}

	if hl == highlightNormalCursor || hl == highlightEditCursor {
		var cursorStyle lipgloss.Style
		if hl == highlightEditCursor {
			cursorStyle = ui.TUI.EditCursor().Inherit(style)
		} else {
			cursorStyle = ui.TUI.NormalCursor().Inherit(style)
		}
		truncated := ansi.Truncate(value, width, symEllipsis)
		styled := cursorStyle.Render(truncated)
		textW := lipgloss.Width(truncated)
		if pad := width - textW; pad > 0 {
			padStr := cursorStyle.Render(strings.Repeat(" ", pad))
			if spec.Align == alignRight {
				return padStr + styled
			}
			return styled + padStr
		}
		return styled
	}

	switch hl {
	case highlightRow:
		style = style.Background(ui.TUI.Palette().Surface).Bold(true)
	case highlightVisual:
		style = ui.TUI.VisualSelected()
	case highlightVisualCursor:
		style = ui.TUI.VisualCursor()
	}

	// Search highlighting: render matched positions with accent style.
	if len(searchPositions) > 0 && hl != highlightVisual && hl != highlightVisualCursor {
		truncated := ansi.Truncate(value, width, symEllipsis)
		highlighted := highlightFuzzyPositions(truncated, searchPositions, style, ui.TUI.TextBlueBold())
		textW := lipgloss.Width(truncated)
		if pad := width - textW; pad > 0 {
			if spec.Align == alignRight {
				return strings.Repeat(" ", pad) + highlighted
			}
			return highlighted + strings.Repeat(" ", pad)
		}
		return highlighted
	}

	aligned := formatCell(value, width, spec.Align)
	return style.Render(aligned)
}

func joinCells(cells []string, separators []string) string {
	if len(cells) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range cells {
		if i > 0 {
			idx := i - 1
			if idx < len(separators) {
				b.WriteString(separators[idx])
			} else if len(separators) > 0 {
				b.WriteString(separators[len(separators)-1])
			}
		}
		b.WriteString(c)
	}
	return b.String()
}

func cellStyle(kind cellKind) lipgloss.Style {
	switch kind {
	case cellReadonly:
		return ui.TUI.Readonly()
	default:
		return ui.TUI.Base()
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], "\r \t")
	}
	return s
}

func formatCell(value string, width int, align alignKind) string {
	pos := lipgloss.Left
	if align == alignRight {
		pos = lipgloss.Right
	}
	return compose.Fit(value, width, pos)
}

func visibleRange(total, height, cursor int) (int, int) {
	if total <= height {
		return 0, total
	}
	cursor = clampCursor(cursor, total)
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

func columnWidths(
	specs []columnSpec,
	rows [][]cell,
	width int,
	separatorWidth int,
	precompNatural []int,
) []int {
	columnCount := len(specs)
	if columnCount == 0 {
		return nil
	}
	available := width - separatorWidth*(columnCount-1)
	if available < columnCount {
		available = columnCount
	}

	natural := precompNatural
	if natural == nil {
		natural = naturalWidths(specs, rows)
	}

	if sumInts(natural) <= available {
		widths := make([]int, columnCount)
		copy(widths, natural)
		extra := available - sumInts(widths)
		if extra > 0 {
			flex := flexColumns(specs)
			if len(flex) == 0 {
				flex = allColumns(specs)
			}
			distribute(widths, specs, flex, extra, true)
		}
		return widths
	}

	// Auto-cap: no column should consume more than 40% of available width
	// unless an explicit Max (from ColHints) says otherwise.
	// Expanded columns bypass the cap and keep their natural width.
	autoCap := available * 40 / 100
	if autoCap < 20 {
		autoCap = 20
	}

	widths := make([]int, columnCount)
	for i, w := range natural {
		if specs[i].Expanded {
			widths[i] = w
			continue
		}
		maxW := specs[i].Max
		if maxW == 0 {
			maxW = autoCap
		}
		if w > maxW {
			w = maxW
		}
		widths[i] = w
	}

	total := sumInts(widths)
	if total <= available {
		extra := available - total
		extra = widenTruncated(widths, natural, extra)
		if extra > 0 {
			flex := flexColumns(specs)
			if len(flex) == 0 {
				flex = allColumns(specs)
			}
			distribute(widths, specs, flex, extra, true)
		}
		return widths
	}

	deficit := total - available
	flex := shrinkableColumns(specs)
	if len(flex) == 0 {
		flex = allColumns(specs)
	}
	distribute(widths, specs, flex, deficit, false)
	return widths
}

func naturalWidths(specs []columnSpec, rows [][]cell) []int {
	return computeNaturalWidths(specs, rows, func(i int) int { return i })
}

func naturalWidthsIndirect(
	specs []columnSpec,
	fullRows [][]cell,
	visToFull []int,
) []int {
	return computeNaturalWidths(
		specs,
		fullRows,
		func(vi int) int { return visToFull[vi] },
	)
}

func computeNaturalWidths(
	specs []columnSpec,
	rows [][]cell,
	colIndex func(int) int,
) []int {
	widths := make([]int, len(specs))
	colCount := len(specs)
	for i, spec := range specs {
		ci := colIndex(i)
		w := headerTitleWidth(spec, colCount)
		for _, row := range rows {
			if ci >= len(row) {
				continue
			}
			value := firstLine(row[ci].Value)
			if value == "" {
				continue
			}
			cw := lipgloss.Width(value)
			if cw > w {
				w = cw
			}
		}
		if w < spec.Min {
			w = spec.Min
		}
		widths[i] = w
	}
	return widths
}

func widenTruncated(widths, natural []int, extra int) int {
	for extra > 0 {
		changed := false
		for i := range widths {
			if extra == 0 {
				break
			}
			if widths[i] < natural[i] {
				widths[i]++
				extra--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return extra
}

func distribute(
	widths []int,
	specs []columnSpec,
	indices []int,
	amount int,
	grow bool,
) {
	if amount <= 0 || len(indices) == 0 {
		return
	}
	for amount > 0 {
		changed := false
		for _, idx := range indices {
			if idx >= len(widths) {
				continue
			}
			if grow {
				if specs[idx].Max > 0 && widths[idx] >= specs[idx].Max {
					continue
				}
				widths[idx]++
			} else {
				if widths[idx] <= specs[idx].Min {
					continue
				}
				widths[idx]--
			}
			amount--
			changed = true
			if amount == 0 {
				break
			}
		}
		if !changed {
			return
		}
	}
}

func flexColumns(specs []columnSpec) []int {
	indices := make([]int, 0, len(specs))
	for i, spec := range specs {
		if spec.Flex {
			indices = append(indices, i)
		}
	}
	return indices
}

// shrinkableColumns returns flex columns that are not expanded.
// Expanded columns keep their natural width and are excluded from shrink/grow distribution.
func shrinkableColumns(specs []columnSpec) []int {
	indices := make([]int, 0, len(specs))
	for i, spec := range specs {
		if spec.Flex && !spec.Expanded {
			indices = append(indices, i)
		}
	}
	return indices
}

func allColumns(specs []columnSpec) []int {
	indices := make([]int, len(specs))
	for i := range specs {
		indices[i] = i
	}
	return indices
}

func sumInts(values []int) int {
	return lo.Sum(values)
}

func safeWidth(widths []int, idx int) int {
	if idx >= len(widths) {
		return 1
	}
	if widths[idx] < 1 {
		return 1
	}
	return widths[idx]
}

const scrollIndicatorWidth = 2

func ensureCursorVisible(tab *Tab, visCursor int, visCount int) {
	if visCount == 0 {
		tab.ViewOffset = 0
		return
	}
	if tab.ViewOffset > visCursor {
		tab.ViewOffset = visCursor
	}
	if tab.ViewOffset > visCount-1 {
		tab.ViewOffset = visCount - 1
	}
	if tab.ViewOffset < 0 {
		tab.ViewOffset = 0
	}
}

func viewportRange(
	widths []int,
	sepWidth int,
	termWidth int,
	viewOffset int,
	visCursor int,
) (start, end int, hasLeft, hasRight bool) {
	n := len(widths)
	if n == 0 {
		return 0, 0, false, false
	}
	if viewOffset < 0 {
		viewOffset = 0
	}
	if viewOffset >= n {
		viewOffset = n - 1
	}

	totalWidth := sumInts(widths)
	if n > 1 {
		totalWidth += (n - 1) * sepWidth
	}
	if totalWidth <= termWidth {
		return 0, n, false, false
	}

	start = viewOffset
	hasLeft = start > 0
	budget := termWidth
	if hasLeft {
		budget -= scrollIndicatorWidth
	}

	end = start
	for end < n {
		colW := widths[end]
		if end > start {
			colW += sepWidth
		}
		if budget-colW < 0 && end > start {
			break
		}
		budget -= colW
		end++
	}

	hasRight = end < n
	if hasRight && budget < scrollIndicatorWidth && end > start+1 {
		end--
	}

	for visCursor >= end && end < n {
		start++
		hasLeft = true
		budget = termWidth - scrollIndicatorWidth
		for e := start; e < n; e++ {
			colW := widths[e]
			if e > start {
				colW += sepWidth
			}
			if budget-colW < 0 && e > start {
				end = e
				break
			}
			budget -= colW
			end = e + 1
		}
		hasRight = end < n
		if hasRight && budget < scrollIndicatorWidth && end > start+1 {
			end--
		}
		if visCursor < end {
			break
		}
	}

	return start, end, hasLeft, hasRight
}

func sliceViewport[T any](items []T, start, end int) []T {
	if start >= len(items) {
		return nil
	}
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func sliceViewportRows(rows [][]cell, start, end int) [][]cell {
	return lo.Map(rows, func(row []cell, _ int) []cell {
		return sliceViewport(row, start, end)
	})
}
