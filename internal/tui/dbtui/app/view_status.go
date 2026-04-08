package app

// view_status.go — Status bar rendering: mode badge, contextual hints with
// progressive compaction (full → compact → dropped) to fit terminal width.

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

func (m *Model) statusView() string {
	if m.hasActiveOverlay() {
		return m.withStatusMessage("")
	}
	if m.search != nil && !m.search.Committed {
		return m.withStatusMessage(m.searchModeStatusHelp())
	}

	navW := lipgloss.Width(m.styles.ModeNormal().Render("NAV"))
	editW := lipgloss.Width(m.styles.ModeEdit().Render("EDIT"))
	visW := lipgloss.Width(m.styles.ModeVisual().Render("VISUAL"))
	badgeWidth := navW
	if editW > badgeWidth {
		badgeWidth = editW
	}
	if visW > badgeWidth {
		badgeWidth = visW
	}
	modeBadge := m.styles.ModeNormal().
		Width(badgeWidth).
		Align(lipgloss.Center).
		Render("NAV")
	switch m.mode {
	case modeEdit:
		modeBadge = m.styles.ModeEdit().
			Width(badgeWidth).
			Align(lipgloss.Center).
			Render("EDIT")
	case modeVisual:
		modeBadge = m.styles.ModeVisual().
			Width(badgeWidth).
			Align(lipgloss.Center).
			Render("VISUAL")
	}

	var help string
	switch m.mode {
	case modeNormal:
		help = m.normalModeStatusHelp(modeBadge)
	case modeEdit:
		help = m.editModeStatusHelp(modeBadge)
	case modeVisual:
		help = m.visualModeStatusHelp(modeBadge)
	}
	return m.withStatusMessage(help)
}

func (m *Model) normalModeHints(modeBadge string) []statusHint {
	hints := []statusHint{
		{ID: "mode", Full: modeBadge, Priority: 0, Required: true},
	}

	writable := !m.currentTabReadOnly()

	// Tier 1 — mode switches and core actions.
	if writable {
		hints = append(hints, statusHint{ID: "edit", Full: m.helpItem(keyI, "edit"), Compact: m.renderKeys(keyI), Priority: 1})
		hints = append(hints, statusHint{ID: "visual", Full: m.helpItem(keyV, "visual"), Compact: m.renderKeys(keyV), Priority: 1})
	}
	hints = append(hints,
		statusHint{ID: "search", Full: m.helpItem(keySlash, "search"), Compact: m.renderKeys(keySlash), Priority: 1},
		statusHint{ID: "sort", Full: m.helpItem(keyS, "sort"), Compact: m.renderKeys(keyS), Priority: 1},
	)

	// Tier 2 — common operations.
	hints = append(hints,
		statusHint{ID: "pin", Full: m.helpItem("space", "pin"), Compact: m.renderKeys("space"), Priority: 2},
		statusHint{ID: "filter", Full: m.helpItem(keyF, "filter"), Compact: m.renderKeys(keyF), Priority: 2},
		statusHint{ID: "preview", Full: m.helpItem(symReturn, "preview"), Compact: m.renderKeys(symReturn), Priority: 2},
	)

	// Tier 3 — less common, first to drop.
	hints = append(hints,
		statusHint{ID: "hide", Full: m.helpItem(keyC, "hide col"), Compact: m.renderKeys(keyC), Priority: 3},
		statusHint{ID: "rename", Full: m.helpItem(keyR, "rename"), Compact: m.renderKeys(keyR), Priority: 3},
		statusHint{ID: "tables", Full: m.helpItem(keyT, "tables"), Compact: m.renderKeys(keyT), Priority: 3},
	)

	// Always last — escape hatch.
	hints = append(hints, statusHint{ID: "help", Full: m.helpItem(keyQuestion, "help"), Compact: m.renderKeys(keyQuestion), Priority: 0, Required: true})
	return hints
}

func (m *Model) normalModeStatusHelp(modeBadge string) string {
	return m.renderStatusHints(m.normalModeHints(modeBadge))
}

func (m *Model) visualModeStatusHelp(modeBadge string) string {
	selCount := 0
	if m.visual != nil {
		selCount = m.explicitVisualSelectionCount()
	}

	hints := []statusHint{
		{ID: "mode", Full: modeBadge, Priority: 0, Required: true},
	}
	if selCount > 0 {
		selLabel := fmt.Sprintf("%d selected", selCount)
		hints = append(hints, statusHint{ID: "sel", Full: m.styles.AccentBold().Render(selLabel), Priority: 0, Required: true})
	}
	hints = append(hints,
		statusHint{ID: "del", Full: m.helpItem(keyD, "delete"), Compact: m.renderKeys(keyD), Priority: 1},
		statusHint{ID: "yank", Full: m.helpItem(keyY, "yank"), Compact: m.renderKeys(keyY), Priority: 1},
		statusHint{ID: "cut", Full: m.helpItem(keyX, "cut"), Compact: m.renderKeys(keyX), Priority: 2},
		statusHint{ID: "paste", Full: m.helpItem(keyP, "paste"), Compact: m.renderKeys(keyP), Priority: 2},
		statusHint{ID: "nav", Full: m.helpItem(keyN, "nav mode"), Compact: m.renderKeys(keyN), Priority: 0, Required: true},
	)
	return m.renderStatusHints(hints)
}

func (m *Model) searchModeStatusHelp() string {
	modeBadge := m.styles.ModeNormal().Render("SEARCH")
	hints := []statusHint{
		{ID: "mode", Full: modeBadge, Priority: 0, Required: true},
		{ID: "nav", Full: m.helpItem("↑/↓", "navigate"), Compact: m.renderKeys("↑/↓"), Priority: 2},
		{ID: "keep", Full: m.helpItem(symReturn, "keep filter"), Compact: m.renderKeys(symReturn), Priority: 1},
		{ID: "close", Full: m.helpItem(keyEsc, "close"), Compact: m.renderKeys(keyEsc), Priority: 0, Required: true},
	}
	return m.renderStatusHints(hints)
}

func (m *Model) editModeStatusHelp(modeBadge string) string {
	hints := []statusHint{
		{ID: "mode", Full: modeBadge, Priority: 0, Required: true},
		{ID: "nav", Full: m.helpItem(keyN, "nav mode"), Compact: m.renderKeys(keyN), Priority: 0, Required: true},
	}
	return m.renderStatusHints(hints)
}

func (m *Model) renderStatusHints(hints []statusHint) string {
	if len(hints) == 0 {
		return ""
	}
	maxW := m.width
	sep := m.helpSeparator()
	compact := make([]bool, len(hints))
	dropped := make([]bool, len(hints))
	maxPriority := 0
	for _, hint := range hints {
		if hint.Priority > maxPriority {
			maxPriority = hint.Priority
		}
	}
	build := func() string {
		parts := make([]string, 0, len(hints))
		for i, hint := range hints {
			if dropped[i] {
				continue
			}
			value := hint.Full
			if compact[i] && hint.Compact != "" {
				value = hint.Compact
			}
			if hint.ID != "" {
				value = m.zones.Mark(zoneHint+hint.ID, value)
			}
			parts = append(parts, value)
		}
		return joinWithSeparator(sep, parts...)
	}

	line := build()
	if lipgloss.Width(line) <= maxW {
		return line
	}

	for _, skipRequired := range []bool{true, false} {
		for priority := maxPriority; priority >= 0; priority-- {
			for i := len(hints) - 1; i >= 0; i-- {
				hint := hints[i]
				if (skipRequired && hint.Required) ||
					hint.Priority != priority || hint.Compact == "" || compact[i] {
					continue
				}
				compact[i] = true
				line = build()
				if lipgloss.Width(line) <= maxW {
					return line
				}
			}
		}
	}

	for priority := maxPriority; priority >= 0; priority-- {
		for i := len(hints) - 1; i >= 0; i-- {
			hint := hints[i]
			if hint.Required || hint.Priority != priority || dropped[i] {
				continue
			}
			dropped[i] = true
			line = build()
			if lipgloss.Width(line) <= maxW {
				return line
			}
		}
	}

	return line
}

func (m *Model) withStatusMessage(helpLine string) string {
	if m.status.Text == "" {
		return helpLine
	}
	style := m.styles.Info()
	if m.status.Kind == statusError {
		style = m.styles.Error()
	}
	return lipgloss.JoinVertical(lipgloss.Left, style.Render(m.status.Text), helpLine)
}
