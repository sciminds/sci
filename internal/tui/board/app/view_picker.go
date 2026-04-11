package app

import "strings"

func (m *Model) viewPicker(width, height int) string {
	if len(m.boards) == 0 {
		return m.styles.PickerHint.Render("no boards yet — create one with `sci board create`")
	}

	var lines []string
	lines = append(lines, m.styles.PickerHint.Render("open a board:"))
	lines = append(lines, "")

	maxItemW := width - 4
	if maxItemW < 8 {
		maxItemW = 8
	}

	for i, id := range m.boards {
		label := truncate(id, maxItemW)
		if i == m.pickerCursor {
			lines = append(lines, m.styles.PickerSelected.Render("▸ "+label))
		} else {
			lines = append(lines, m.styles.PickerItem.Render("  "+label))
		}
		if len(lines) >= height {
			break
		}
	}
	return strings.Join(lines, "\n")
}
