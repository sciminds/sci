package tui

// views.go — Bubble Tea View() helpers for each phase: selecting templates,
// applying changes, and the final done/error summary.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/ui"
)

// ── Done phase ───────────────────────────────────────────────────────────────

func (m Model) viewDone() string {
	var lines []string

	if m.Err != nil {
		lines = append(lines, ui.StatusRow(ui.TUI.Fail().Render(ui.IconFail), m.Err.Error()))
		return strings.Join(lines, "\n")
	}

	var applied, skipped int
	for _, f := range m.files {
		if f.applied {
			applied++
			icon := ui.TUI.Pass().Render(ui.IconPass)
			label := f.statusLabel()
			lines = append(lines, ui.StatusRow(icon, f.file.Path+"  "+ui.TUI.Dim().Render(label)))
		} else {
			skipped++
			lines = append(lines, ui.StatusRow(
				ui.TUI.Dim().Render(ui.IconSkip),
				ui.TUI.Dim().Render(f.file.Path+" skipped"),
			))
		}
	}

	lines = append(lines, ui.TUI.RenderDivider(ui.ContentWidth(m.width)))

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
		body = m.spinner.View() + ui.TUI.FgAccent().Render(fmt.Sprintf("Applying %d files…", m.selectList.SelectedCount()))
	case phaseDone:
		body = m.viewDone()
	}

	v := tea.NewView(ui.PageLayout("sci proj config", body, m.footerLeft(), m.footerRight(), m.width))
	v.AltScreen = true
	return v
}
