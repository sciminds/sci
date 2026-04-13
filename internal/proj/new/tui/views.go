package tui

// views.go — Bubble Tea View() helpers for each phase: selecting templates,
// applying changes, and the final done/error summary.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/uikit"
	"github.com/sciminds/cli/internal/ui"
)

// ── Done phase ───────────────────────────────────────────────────────────────

func (m Model) viewDone() string {
	var lines []string

	if m.Err != nil {
		lines = append(lines, ui.StatusRow(uikit.TUI.Fail().Render(uikit.IconFail), m.Err.Error()))
		return strings.Join(lines, "\n")
	}

	fileLines := lo.Map(m.files, func(f fileEntry, _ int) string {
		if f.applied {
			icon := uikit.TUI.Pass().Render(uikit.IconPass)
			label := f.statusLabel()
			return ui.StatusRow(icon, f.file.Path+"  "+uikit.TUI.Dim().Render(label))
		}
		return ui.StatusRow(
			uikit.TUI.Dim().Render(uikit.IconSkip),
			uikit.TUI.Dim().Render(f.file.Path+" skipped"),
		)
	})
	lines = append(lines, fileLines...)

	applied := lo.CountBy(m.files, func(f fileEntry) bool { return f.applied })
	skipped := len(m.files) - applied

	lines = append(lines, uikit.TUI.RenderDivider(uikit.ContentWidth(m.width)))

	lines = append(lines, ui.SummaryLine(
		ui.SummaryPart{Count: applied, Label: "applied", Kind: ui.SummarySuccess},
		ui.SummaryPart{Count: skipped, Label: "skipped", Kind: ui.SummaryDim},
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

	v := tea.NewView(ui.PageLayout("sci proj config", body, m.footerLeft(), m.footerRight(), m.width))
	v.AltScreen = true
	return v
}
