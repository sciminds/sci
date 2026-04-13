package tui

// views.go — Bubble Tea View() helpers for each phase: selecting templates,
// applying changes, and the final done/error summary.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
)

// ── Done phase ───────────────────────────────────────────────────────────────

func (m Model) viewDone() string {
	var lines []string

	if m.Err != nil {
		lines = append(lines, uikit.StatusRow(uikit.TUI.Fail().Render(uikit.IconFail), m.Err.Error()))
		return strings.Join(lines, "\n")
	}

	fileLines := lo.Map(m.files, func(f fileEntry, _ int) string {
		if f.applied {
			icon := uikit.TUI.Pass().Render(uikit.IconPass)
			label := f.statusLabel()
			return uikit.StatusRow(icon, f.file.Path+"  "+uikit.TUI.Dim().Render(label))
		}
		return uikit.StatusRow(
			uikit.TUI.Dim().Render(uikit.IconSkip),
			uikit.TUI.Dim().Render(f.file.Path+" skipped"),
		)
	})
	lines = append(lines, fileLines...)

	applied := lo.CountBy(m.files, func(f fileEntry) bool { return f.applied })
	skipped := len(m.files) - applied

	lines = append(lines, uikit.TUI.RenderDivider(uikit.ContentWidth(m.width)))

	lines = append(lines, uikit.SummaryLine(
		uikit.SummaryPart{Count: applied, Label: "applied", Kind: uikit.SummarySuccess},
		uikit.SummaryPart{Count: skipped, Label: "skipped", Kind: uikit.SummaryDim},
	))

	return strings.Join(lines, "\n")
}

// ── View ─────────────────────────────────────────────────────────────────────

// View implements tea.Model.
func (m Model) View() tea.View {
	var body string
	switch m.phase {
	case phaseSelecting:
		body = m.selectList.View()
	case phaseApplying:
		body = m.spinner.View() + uikit.TUI.TextBlue().Render(fmt.Sprintf("Applying %d files…", m.selectList.SelectedCount()))
	case phaseDone:
		body = m.viewDone()
	}

	v := tea.NewView(uikit.PageLayout("sci proj config", body, m.footerLeft(), m.footerRight(), m.width))
	v.AltScreen = true
	return v
}
