package app

import (
	"fmt"
	"strings"
)

func (m *Model) viewDetail(width, height int) string {
	card := m.focusedCard()
	if card == nil {
		return m.styles.Help.Render("  no card selected")
	}

	innerW := width - 6 // frame borders + padding
	if innerW < 20 {
		innerW = 20
	}

	var lines []string
	lines = append(lines, m.styles.DetailHeading.Render(truncate(card.Title, innerW)))
	lines = append(lines, "")

	if card.Description != "" {
		for _, l := range strings.Split(card.Description, "\n") {
			lines = append(lines, m.styles.DetailBody.Render(truncate(l, innerW)))
		}
		lines = append(lines, "")
	}

	lines = append(lines, m.styles.DetailSection.Render("labels"))
	if len(card.Labels) == 0 {
		lines = append(lines, m.styles.Help.Render("  —"))
	} else {
		lines = append(lines, "  "+strings.Join(card.Labels, ", "))
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.DetailSection.Render("assignees"))
	if len(card.Assignees) == 0 {
		lines = append(lines, m.styles.Help.Render("  —"))
	} else {
		lines = append(lines, "  "+strings.Join(card.Assignees, ", "))
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.DetailSection.Render("checklist"))
	if len(card.Checklist) == 0 {
		lines = append(lines, m.styles.Help.Render("  —"))
	} else {
		for _, ci := range card.Checklist {
			mark := "○"
			if ci.Done {
				mark = "●"
			}
			lines = append(lines, fmt.Sprintf("  %s %s", mark, truncate(ci.Text, innerW-4)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, m.styles.DetailSection.Render("comments"))
	if len(card.Comments) == 0 {
		lines = append(lines, m.styles.Help.Render("  —"))
	} else {
		for _, c := range card.Comments {
			lines = append(lines, m.styles.CardMeta.Render(fmt.Sprintf("  %s · %s", c.Author, c.Ts.Format("2006-01-02 15:04"))))
			for _, l := range strings.Split(c.Text, "\n") {
				lines = append(lines, "  "+truncate(l, innerW-2))
			}
		}
	}

	// Clip/pad to interior height: subtract 2 borders + 2 vertical padding
	// rows (DetailFrame uses Padding(1, 2)). lipgloss .Width sets the total
	// rendered width, so pass width directly — see view_grid.go for the
	// same gotcha.
	innerH := height - 4
	if innerH < 3 {
		innerH = 3
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	for len(lines) < innerH {
		lines = append(lines, "")
	}

	return m.styles.DetailFrame.Width(width).Render(strings.Join(lines, "\n"))
}
