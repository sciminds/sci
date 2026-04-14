package app

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

func (m *Model) viewDetail(width, height int) string {
	card := m.focusedCard()
	if card == nil {
		return m.styles.Help.Render("  no card selected")
	}

	return uikit.Box(width, height, m.styles.DetailFrame, func(innerW, innerH int) string {
		if innerW < 20 {
			innerW = 20
		}
		if innerH < 3 {
			innerH = 3
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

		return uikit.FitHeight(strings.Join(lines, "\n"), innerH)
	})
}
