package app

// table_list_render.go — rendering for the table list overlay: the main table
// list view and action hint bar.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// Overlay dimension bounds and layout constants for the table list and file browser.
const (
	tableListMinW               = 24
	tableListMaxW               = 50
	fileBrowserMinW             = 40
	fileBrowserMaxW             = 70
	tableListNameAlignReserve   = 22 // space for shape label "virtual · N × N"
	fileBrowserNameAlignReserve = 12 // space for size label

	// Extra chrome lines beyond OverlayChromeLines for each overlay type.
	// header+path(1) + blanks(3) + status(1) + hints(2) = 7
	tableListExtraChrome   = 7
	fileBrowserExtraChrome = 7

	// Derive textarea sizing.
	deriveSQLWidthInset = 6  // OverlayBoxPadding(4) + textarea border(2)
	deriveSQLMinH       = 4  // minimum SQL textarea height
	deriveSQLChrome     = 14 // lines consumed by header, labels, name field, blanks, hints
)

// buildTableListOverlay renders the table list modal.
func (m *Model) buildTableListOverlay() string {
	tl := m.tableList
	if tl == nil {
		return ""
	}

	// Use a wider overlay when in file-picker mode to give paths room.
	minW, maxW := tableListMinW, tableListMaxW
	if tl.Adding {
		minW, maxW = fileBrowserMinW, fileBrowserMaxW
	}
	contentW := ui.OverlayWidth(m.width, minW, maxW)
	innerW := contentW - ui.OverlayBoxPadding

	if tl.Adding {
		return m.buildAddFileOverlay(contentW, innerW)
	}

	if tl.Creating {
		return m.buildCreateTableOverlay(contentW, innerW)
	}

	if tl.Deriving {
		return m.buildDeriveOverlay(contentW)
	}

	var b strings.Builder
	b.WriteString(m.styles.HeaderSection().Render(" Tables "))

	// Show db path.
	if m.dbPath != "" {
		path := truncateLeft(shortenHome(m.dbPath), innerW)
		b.WriteString("  ")
		b.WriteString(m.styles.HeaderHint().Render(path))
	}
	b.WriteString("\n\n")

	if len(tl.Tables) == 0 {
		b.WriteString(m.styles.Empty().Render("No tables"))
		b.WriteString("\n")
	} else {
		// Compute max name width for alignment.
		maxNameW := 0
		for _, e := range tl.Tables {
			if len(e.Name) > maxNameW {
				maxNameW = len(e.Name)
			}
		}
		if maxNameW > innerW-tableListNameAlignReserve {
			maxNameW = innerW - tableListNameAlignReserve
		}

		maxVisible := ui.OverlayBodyHeight(m.height, tableListExtraChrome)
		if maxVisible > len(tl.Tables) {
			maxVisible = len(tl.Tables)
		}
		start := tl.Cursor - maxVisible/2
		if start < 0 {
			start = 0
		}
		end := start + maxVisible
		if end > len(tl.Tables) {
			end = len(tl.Tables)
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}

		for i := start; i < end; i++ {
			entry := tl.Tables[i]
			selected := i == tl.Cursor

			name := entry.Name
			if len(name) > maxNameW {
				name = name[:maxNameW-1] + symEllipsis
			}

			shape := fmt.Sprintf("%d × %d", entry.Rows, entry.Columns)
			if entry.IsView {
				shape = "view · " + shape
			} else if entry.IsVirtual {
				shape = "virtual · " + shape
			}
			shapeStyled := m.styles.HeaderHint().Render(shape)

			// Pad name to align shape column.
			padLen := maxNameW - len(entry.Name)
			if padLen < 0 {
				padLen = 0
			}
			padding := strings.Repeat(" ", padLen+2)

			if selected && tl.Renaming {
				// Show textinput for rename
				pointer := m.styles.AccentBold().Render(symTriRight + " ")
				line := pointer + tl.RenameInput.View()
				b.WriteString(line)
			} else if selected {
				pointer := m.styles.AccentBold().Render(symTriRight + " ")
				nameStyled := m.styles.AccentBold().Render(name)
				line := pointer + nameStyled + padding + shapeStyled
				if lipgloss.Width(line) > innerW {
					line = m.styles.Base().MaxWidth(innerW).Render(line)
				}
				b.WriteString(line)
			} else {
				line := "  " + name + padding + shapeStyled
				if lipgloss.Width(line) > innerW {
					line = m.styles.Base().MaxWidth(innerW).Render(line)
				}
				b.WriteString(line)
			}
			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}

	// Status message (e.g. "Dropped table_x"). Always reserve the line to
	// avoid the overlay height jumping when a status appears/disappears.
	b.WriteString("\n\n")
	if tl.Status != "" {
		b.WriteString(m.styles.Info().Render(tl.Status))
	} else {
		b.WriteString(" ")
	}
	b.WriteString("\n\n")

	// Action hints — wrap to innerW so keys don't get clipped.
	b.WriteString(m.tableListHints(innerW))

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}

// tableListHints renders action key hints, wrapping to fit within maxW.
func (m *Model) tableListHints(maxW int) string {
	var hints []string
	tl := m.tableList
	if tl.Renaming {
		hints = []string{
			m.helpItem(keyEnter, "confirm"),
			m.helpItem(keyEsc, "cancel"),
		}
	} else {
		hints = []string{
			m.helpItem(symReturn, "switch"),
			m.helpItem(keyR, "rename"),
			m.helpItem(keyD, "delete"),
			m.helpItem(keyE, "export"),
			m.helpItem(keyA, "add"),
			m.helpItem(keyC, "create"),
			m.helpItem(keyEsc, "close"),
		}
	}

	sep := m.helpSeparator()
	sepW := lipgloss.Width(sep)

	// Greedily pack hints into lines that fit within maxW.
	var lines []string
	var lineItems []string
	lineW := 0
	for _, h := range hints {
		hw := lipgloss.Width(h)
		needed := hw
		if len(lineItems) > 0 {
			needed += sepW
		}
		if lineW+needed > maxW && len(lineItems) > 0 {
			lines = append(lines, joinWithSeparator(sep, lineItems...))
			lineItems = nil
			lineW = 0
		}
		lineItems = append(lineItems, h)
		if lineW == 0 {
			lineW = hw
		} else {
			lineW += sepW + hw
		}
	}
	if len(lineItems) > 0 {
		lines = append(lines, joinWithSeparator(sep, lineItems...))
	}
	return strings.Join(lines, "\n")
}
