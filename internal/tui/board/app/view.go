package app

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// View is the top-level render. It composes chrome (title + body + status)
// and delegates the body to the active screen's renderer.
func (m *Model) View() tea.View {
	v := tea.NewView(m.buildView())
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) buildView() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	chrome := uikit.Chrome{
		Title: func(w int) string {
			raw := m.router.Title(m.screen, m, w)
			return m.styles.Title.Render(truncate(raw, w-2))
		},
		Status: func(w int) string {
			text := m.status.text
			if text == "" {
				text = m.router.Help(m.screen)
			}
			style := m.styles.Status
			if m.status.kind == statusError && m.status.text != "" {
				style = m.styles.StatusErr
			}
			return style.Render(truncate(text, w-2))
		},
		Body: func(w, h int) string {
			return m.router.View(m.screen, m, w, h)
		},
	}

	return chrome.Render(m.width, m.height)
}

// truncate cuts s to at most n visible cells, appending … if it overflowed.
// Zero or negative n returns empty string.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	// Cheap rune truncation — good enough for ASCII-dominant UI strings.
	// Replace with ansi-aware cutter if styled text needs trimming.
	r := []rune(s)
	if len(r) > n-1 {
		r = r[:n-1]
	}
	return string(r) + "…"
}
