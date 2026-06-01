package app

// table_list_render.go — rendering for the table list overlay: the main table
// list view and action hint bar.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
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
	// header+path(1) + blanks(3) + filter(1) + blank(1) + status(1) + hints(2) = 9
	tableListExtraChrome   = 9
	fileBrowserExtraChrome = 7

	// Derive textarea sizing.
	deriveSQLWidthInset = 8  // OverlayBoxPadding(6) + textarea border(2)
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
	contentW := uikit.OverlayWidth(m.width, minW, maxW)
	innerW := contentW - uikit.OverlayBoxPadding

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

	// Filter line — always reserved so the box height stays stable whether or
	// not a filter is active (see tableListExtraChrome).
	b.WriteString(m.tableListFilterLine(innerW))
	b.WriteString("\n\n")

	vis := tl.visibleMatches()
	if len(vis) == 0 {
		empty := "No tables"
		if tl.Query != "" {
			empty = "No tables match"
		}
		b.WriteString(m.styles.Empty().Render(empty))
		b.WriteString("\n")
	} else {
		// Compute max name width for alignment across the visible entries.
		maxNameW := 0
		for _, mt := range vis {
			if l := len(tl.Tables[mt.Index].Name); l > maxNameW {
				maxNameW = l
			}
		}
		if maxNameW > innerW-tableListNameAlignReserve {
			maxNameW = innerW - tableListNameAlignReserve
		}

		maxVisible := min(uikit.OverlayBodyHeight(m.height, tableListExtraChrome), len(vis))
		cursor := min(tl.Cursor, len(vis)-1)
		start := max(cursor-maxVisible/2, 0)
		end := start + maxVisible
		if end > len(vis) {
			end = len(vis)
			start = max(end-maxVisible, 0)
		}

		for vi := start; vi < end; vi++ {
			mt := vis[vi]
			entry := tl.Tables[mt.Index]
			selected := vi == cursor

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

			if selected && tl.Renaming {
				// Show textinput for rename
				pointer := m.styles.TextBlueBold().Render(symTriRight + " ")
				line := pointer + tl.RenameInput.View()
				b.WriteString(line)
			} else if selected {
				pointer := m.styles.TextBlueBold().Render(symTriRight + " ")
				nameStyled := m.styles.TextBlueBold().Render(name)
				paddedStyled := uikit.PadRight(nameStyled, maxNameW+2)
				line := pointer + paddedStyled + shapeStyled
				if lipgloss.Width(line) > innerW {
					line = m.styles.Base().MaxWidth(innerW).Render(line)
				}
				b.WriteString(line)
			} else {
				// Highlight the fuzzy-matched characters in unselected rows.
				positions := clampPositions(mt.Positions, len([]rune(name)))
				nameStyled := highlightFuzzyPositions(name, positions, m.styles.Base(), m.styles.TextBlueBold())
				paddedName := uikit.PadRight(nameStyled, maxNameW+2)
				line := "  " + paddedName + shapeStyled
				if lipgloss.Width(line) > innerW {
					line = m.styles.Base().MaxWidth(innerW).Render(line)
				}
				b.WriteString(line)
			}
			if vi < end-1 {
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

// tableListFilterLine renders the / filter row. While typing it shows the
// live input; once committed it shows the query and the match count; when no
// filter is active it reserves a blank line so the box height stays stable.
func (m *Model) tableListFilterLine(innerW int) string {
	tl := m.tableList
	prompt := m.styles.Keycap().Render("/")

	var line string
	switch {
	case tl.Filtering:
		count := m.styles.HeaderHint().Render(fmt.Sprintf(" %d/%d", len(tl.visibleMatches()), len(tl.Tables)))
		line = prompt + " " + tl.FilterInput.View() + count
	case tl.Query != "":
		count := m.styles.HeaderHint().Render(fmt.Sprintf(" %d/%d", len(tl.visibleMatches()), len(tl.Tables)))
		line = prompt + " " + tl.Query + count
	default:
		return " " // reserve the line so the box height stays stable
	}

	if lipgloss.Width(line) > innerW {
		line = m.styles.Base().MaxWidth(innerW).Render(line)
	}
	return line
}

// clampPositions drops match positions that fall outside [0, n), which can
// happen after a long name is truncated for display.
func clampPositions(positions []int, n int) []int {
	return lo.Filter(positions, func(p int, _ int) bool {
		return p >= 0 && p < n
	})
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
	} else if tl.Filtering {
		hints = []string{
			m.helpItem(keyEnter, "apply"),
			m.helpItem(keyEsc, "clear"),
		}
	} else {
		hints = []string{
			m.helpItem(symReturn, "switch"),
			m.helpItem(keySlash, "filter"),
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
