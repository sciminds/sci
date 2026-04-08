package app

// view.go — Bubble Tea View() implementation: composes the tab bar, table
// grid, status line, and overlay stack into the final frame.
//
// Rendering sub-components live in sibling files:
//
//   - view_table.go   — table grid layout and cell rendering
//   - view_status.go  — status bar and mode hints
//   - view_helpers.go — text layout utilities and simple overlay builders

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

const (
	loadingChromeLines = 4 // blank + tab + underline + status
	minLoadingBodyH    = 3
	emptyDBTopChrome   = 1 // top-line chrome in empty DB view

	// Overlay dimension bounds for help.
	helpOverlayMinW = 30
	helpOverlayMaxW = 70
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.zones.Scan(m.buildView()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) buildView() string {
	if m.width < ui.MinUsableWidth || m.height < ui.MinUsableHeight {
		return m.buildTerminalTooSmallView()
	}

	base := m.buildBaseView()

	// Overlay stack.
	overlays := []struct {
		active bool
		render func() string
	}{
		{m.cellEditor != nil, m.buildCellEditorOverlay},
		{m.columnRename != nil, m.buildColumnRenameOverlay},
		{m.notePreview != nil, m.buildNotePreviewOverlay},
		{m.tableList != nil, m.buildTableListOverlay},
		{m.columnPicker != nil, m.buildColumnPickerOverlay},
		{m.helpVisible, m.buildHelpOverlay},
	}

	hasOverlay := false
	for _, o := range overlays {
		if o.active {
			hasOverlay = true
			break
		}
	}

	if hasOverlay {
		if lines := strings.Split(base, "\n"); len(lines) > m.height {
			base = strings.Join(lines[len(lines)-m.height:], "\n")
		}
	}

	for _, o := range overlays {
		if o.active {
			fg := m.zones.Mark(zoneOverlay, ui.CancelFaint(o.render()))
			base = ui.CenterOverlay(fg, ui.DimBackground(base))
		}
	}

	return base
}

func (m *Model) buildTerminalTooSmallView() string {
	panel := lipgloss.JoinVertical(
		lipgloss.Center,
		m.styles.Error().Render("Terminal too small"),
		"",
		m.styles.HeaderHint().Render(
			fmt.Sprintf(
				"%dx%d \u2014 need at least %dx%d",
				m.width, m.height,
				ui.MinUsableWidth, ui.MinUsableHeight,
			),
		),
	)
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		clampLines(panel, m.width),
	)
}

func (m *Model) buildBaseView() string {
	if len(m.tabs) == 0 {
		return m.buildEmptyDBView()
	}

	tabs := m.tabsView()
	tabLine := m.tabUnderline()

	var content string
	if tab := m.effectiveTab(); tab != nil {
		content = m.tableView(tab)
	} else if m.active >= 0 && m.active < len(m.tabs) && m.tabs[m.active].Loading {
		content = m.buildLoadingView()
	}
	status := m.statusView()

	// Right-align db path on the tab row.
	if m.dbPath != "" {
		tabsW := lipgloss.Width(tabs)
		available := m.width - tabsW - 2
		if available > 5 {
			path := truncateLeft(shortenHome(m.dbPath), available)
			if strings.HasPrefix(m.dbPath, "/") || strings.HasPrefix(m.dbPath, "~") {
				path = osc8Link("file://"+m.dbPath, path)
			}
			label := m.styles.HeaderHint().Render(path)
			gap := m.width - tabsW - lipgloss.Width(label)
			if gap > 0 {
				tabs += strings.Repeat(" ", gap) + label
			}
		}
	}

	upper := lipgloss.JoinVertical(lipgloss.Left, "", tabs, tabLine)
	if content != "" {
		upper = lipgloss.JoinVertical(lipgloss.Left, upper, content)
	}

	upperH := lipgloss.Height(upper)
	statusH := lipgloss.Height(status)
	gap := m.height - upperH - statusH + 1
	if gap < 1 {
		gap = 1
	}

	var b strings.Builder
	b.WriteString(upper)
	b.WriteString(strings.Repeat("\n", gap))
	b.WriteString(status)
	return clampLines(b.String(), m.width)
}

func (m *Model) tabsView() string {
	pinned := m.mode == modeEdit || m.mode == modeVisual

	// Render a single tab (name + filter mark + gap).
	renderTab := func(i int) string {
		tab := m.tabs[i]
		var rendered string
		if i == m.active {
			rendered = m.styles.TabActive().Render(tab.Name)
		} else if pinned {
			rendered = m.styles.TabLocked().Render(tab.Name)
		} else {
			rendered = m.styles.TabInactive().Render(tab.Name)
		}
		rendered = m.zones.Mark(fmt.Sprintf("%s%d", zoneTab, i), rendered)
		var mark string
		switch {
		case tab.FilterActive && tab.FilterInverted:
			mark = filterMarkActiveInverted
		case tab.FilterActive:
			mark = filterMarkActive
		case tab.FilterInverted:
			mark = filterMarkPreviewInverted
		case len(tab.Pins) > 0:
			mark = filterMarkPreview
		}
		if mark != "" {
			rendered += " " + m.styles.FilterMark().Render(mark) + " "
		} else {
			rendered += "   "
		}
		return rendered
	}

	tabWidth := func(i int) int {
		return lipgloss.Width(renderTab(i))
	}

	// Reserve space for the db path on the right (handled by buildBaseView).
	// Use ~80% of terminal width for tabs to leave room for the path label.
	maxTabsWidth := m.width * 4 / 5
	if maxTabsWidth < 40 {
		maxTabsWidth = m.width - 4
	}

	// Find the range of tabs that fit, centered around the active tab.
	lo, hi := m.active, m.active
	used := tabWidth(m.active)

	// Expand alternately left and right.
	for {
		expanded := false
		if lo > 0 {
			w := tabWidth(lo - 1)
			if used+w <= maxTabsWidth {
				lo--
				used += w
				expanded = true
			}
		}
		if hi < len(m.tabs)-1 {
			w := tabWidth(hi + 1)
			if used+w <= maxTabsWidth {
				hi++
				used += w
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	var parts []string

	// Left overflow indicator.
	if lo > 0 {
		indicator := m.styles.TextDim().Render(fmt.Sprintf("◀ %d ", lo))
		parts = append(parts, indicator)
	}

	for i := lo; i <= hi; i++ {
		parts = append(parts, renderTab(i))
	}

	// Right overflow indicator.
	if hi < len(m.tabs)-1 {
		remaining := len(m.tabs) - 1 - hi
		indicator := m.styles.TextDim().Render(fmt.Sprintf(" %d ▶", remaining))
		parts = append(parts, indicator)
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m *Model) tabUnderline() string {
	style := m.styles.TabUnderline()
	switch m.mode {
	case modeEdit:
		style = m.styles.SecondaryText()
	case modeVisual:
		style = m.styles.FilterMark() // muted color
	}
	return style.Render(strings.Repeat("\u2501", m.width))
}

func (m *Model) tableView(tab *Tab) string {
	if tab == nil || len(tab.Specs) == 0 {
		return ""
	}

	normalSep := m.styles.TableSeparator().Render(" \u2502 ")
	normalDiv := m.styles.TableSeparator().Render("\u2500\u253c\u2500")
	sepW := lipgloss.Width(normalSep)

	vp := m.tabViewport(tab)
	if len(vp.Specs) == 0 {
		return ""
	}
	header := renderHeaderRow(
		vp.Specs, vp.Widths, vp.CollapsedSeps, vp.Cursor,
		vp.Sorts, vp.HasLeft, vp.HasRight, m.zones, zoneCol,
	)
	divider := renderDivider(vp.Widths, vp.PlainSeps, normalDiv, m.styles.TableSeparator())

	badges := renderHiddenBadges(tab.Specs, tab.ColCursor)
	badgeChrome := 0
	if badges != "" {
		badgeChrome = 1
	}
	rowCountChrome := 0
	if len(tab.Rows) > 0 {
		rowCountChrome = 1
	}

	effectiveHeight := tab.Table.Height() - badgeChrome - rowCountChrome
	if effectiveHeight < 2 {
		effectiveHeight = 2
	}

	pinCtx := m.viewportPinContext(tab, vp)
	var visualSel map[int]bool
	if m.mode == modeVisual && m.visual != nil {
		visualSel = m.visualSelectionSet()
	}

	// Build search highlight map projected to viewport columns.
	var searchHL map[int]map[int][]int
	if m.search != nil && m.search.Highlights != nil {
		searchHL = projectSearchHighlights(m.search.Highlights, vp.VisToFull)
	}

	// Show per-cell cursor in both normal and edit modes.
	// Only suppress in visual mode (row-only selection).
	renderColCursor := vp.Cursor
	if m.mode == modeVisual {
		renderColCursor = -1
	}

	rows := renderRows(
		vp.Specs, vp.Cells, tab.Rows, vp.Widths,
		vp.PlainSeps, vp.CollapsedSeps,
		tab.Table.Cursor(), renderColCursor, m.mode == modeEdit,
		effectiveHeight, pinCtx, m.zones, zoneRow,
		visualSel, searchHL,
	)

	var bodyParts []string
	if m.search != nil && !m.search.Committed {
		bodyParts = append(bodyParts, m.renderSearchBar())
	}
	bodyParts = append(bodyParts, header, divider)
	if len(rows) == 0 {
		if tab.FilterActive && tabstate.HasPins(tab) {
			bodyParts = append(bodyParts, m.styles.Empty().Render("No matches."))
		} else {
			bodyParts = append(bodyParts, m.styles.Empty().Render("Empty table."))
		}
	} else {
		bodyParts = append(bodyParts, strings.Join(rows, "\n"))
	}
	if badges != "" {
		tableWidth := sumInts(vp.Widths)
		if len(vp.Widths) > 1 {
			tableWidth += (len(vp.Widths) - 1) * sepW
		}
		centered := lipgloss.PlaceHorizontal(tableWidth, lipgloss.Center, badges)
		bodyParts = append(bodyParts, centered)
	}
	if n := len(tab.Rows); n > 0 {
		label := fmt.Sprintf("%d rows", n)
		if n == 1 {
			label = "1 row"
		}
		bodyParts = append(bodyParts, m.styles.Empty().Render(label))
	}
	return joinVerticalNonEmpty(bodyParts...)
}

func (m *Model) viewportPinContext(tab *Tab, vp tableViewport) pinRenderContext {
	if !tabstate.HasPins(tab) {
		return pinRenderContext{}
	}
	fullToVP := make(map[int]int, len(vp.VisToFull))
	for vpIdx, fullIdx := range vp.VisToFull {
		fullToVP[fullIdx] = vpIdx
	}
	var translated []filterPin
	for _, pin := range tab.Pins {
		if vpIdx, ok := fullToVP[pin.Col]; ok {
			translated = append(translated, filterPin{
				Col:    vpIdx,
				Values: pin.Values,
			})
		}
	}
	return pinRenderContext{
		Pins:     translated,
		RawCells: vp.Cells,
		Inverted: tab.FilterInverted,
	}
}

func (m *Model) buildLoadingView() string {
	name := m.tabs[m.active].Name
	label := m.spinner.View() + " " + m.styles.AccentBold().Render("Loading "+name+"…")
	bodyH := m.height - loadingChromeLines
	if bodyH < minLoadingBodyH {
		bodyH = minLoadingBodyH
	}
	return lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, label)
}

func (m *Model) buildEmptyDBView() string {
	panel := lipgloss.JoinVertical(
		lipgloss.Center,
		m.styles.HeaderSection().Render(" Empty database "),
		"",
		m.styles.HeaderHint().Render("Press "+m.keycap(keyT)+" to manage tables"),
	)
	// Right-align db path in the top-right corner.
	var topLine string
	if m.dbPath != "" {
		path := truncateLeft(shortenHome(m.dbPath), m.width-2)
		topLine = lipgloss.PlaceHorizontal(m.width, lipgloss.Right, m.styles.HeaderHint().Render(path))
	}
	body := lipgloss.Place(
		m.width, m.height-emptyDBTopChrome,
		lipgloss.Center, lipgloss.Center,
		clampLines(panel, m.width),
	)
	if topLine != "" {
		return topLine + "\n" + body
	}
	return body
}
