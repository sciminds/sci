package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/tui/board/ui"
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

	body := m.renderBody()
	title := m.renderTitle()
	status := m.renderStatus()

	return lipgloss.JoinVertical(lipgloss.Left, title, body, status)
}

func (m *Model) renderTitle() string {
	var title string
	switch m.screen {
	case screenPicker:
		title = "sci board"
	case screenGrid:
		title = "sci board · " + m.current.Title
	case screenDetail:
		if c := m.focusedCard(); c != nil {
			title = "sci board · " + m.current.Title + " · " + c.Title
		} else {
			title = "sci board · " + m.current.Title
		}
	}
	title = truncate(title, m.width-2)
	return m.styles.Title.Render(title)
}

func (m *Model) renderStatus() string {
	text := m.status.text
	if text == "" {
		text = m.renderHelpHint()
	}
	style := m.styles.Status
	if m.status.kind == statusError && m.status.text != "" {
		style = m.styles.StatusErr
	}
	return style.Render(truncate(text, m.width-2))
}

func (m *Model) renderHelpHint() string {
	switch m.screen {
	case screenPicker:
		return "j/k move  ↵ open  r reload  q quit"
	case screenGrid:
		return "hjkl move  c collapse  C expand  tab switch board  ↵ detail  esc back  q quit"
	case screenDetail:
		return "esc back  q grid"
	}
	return ""
}

func (m *Model) renderBody() string {
	// Reserve space for chrome: title + status bars.
	bodyH := m.height - ui.TitleLines - ui.StatusLines
	if bodyH < 1 {
		bodyH = 1
	}
	bodyW := m.width

	var raw string
	switch m.screen {
	case screenPicker:
		raw = m.viewPicker(bodyW, bodyH)
	case screenGrid:
		raw = m.viewGrid(bodyW, bodyH)
	case screenDetail:
		raw = m.viewDetail(bodyW, bodyH)
	}

	// Pad/truncate body to exact height so status bar lands on the last row.
	lines := strings.Split(raw, "\n")
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
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
