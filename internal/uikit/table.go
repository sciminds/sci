// table.go — the canonical static (non-interactive) table renderer for CLI
// command output. `sci db` verbs and any other command that prints tabular
// results route through [RenderTable] so the look (borders, header, ellipsis
// truncation) stays consistent with the rest of the TUI styling.
//
// This is the static counterpart to the interactive table in
// internal/tui/dbtui — same visual language, but rendered once to a string
// instead of driven by a bubbletea model.

package uikit

import (
	"os"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"golang.org/x/term"
)

// TableOptions tunes [RenderTable].
type TableOptions struct {
	// Width is the maximum total render width in cells. Cells are truncated
	// with an ellipsis so no line exceeds it. Width <= 0 disables truncation
	// entirely (full content) — the right default when output is piped and a
	// downstream tool needs complete cells. Pass [TermWidth] for the live
	// terminal width.
	Width int
	// RightAlign optionally right-aligns specific columns (numeric data). A
	// nil or short slice left-aligns the remaining columns.
	RightAlign []bool
}

// cellPadding is the horizontal padding applied to every cell (one space on
// each side), matching duckdb's box rendering.
const cellPadding = 1

// minColWidth is the floor a column is shrunk to before truncation stops
// stealing width from it — below this an ellipsis is all that's left.
const minColWidth = 3

// TermWidth reports the width of the controlling terminal in cells, or 0 when
// stdout is not a terminal (piped, redirected, captured in tests). A 0 result
// is the signal to [RenderTable] to skip truncation.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// RenderTable renders headers + rows as a bordered table using the shared
// uikit styling. Columns are sized to their content; when opts.Width > 0 and
// the natural table would overflow, the widest columns are shrunk and their
// cells truncated with an ellipsis until the whole table fits.
func RenderTable(headers []string, rows [][]string, opts TableOptions) string {
	if len(headers) == 0 {
		return ""
	}
	nCols := len(headers)

	// Natural width of each column = the widest cell (header or body).
	widths := make([]int, nCols)
	for c, h := range headers {
		widths[c] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for c := 0; c < nCols && c < len(row); c++ {
			if w := lipgloss.Width(row[c]); w > widths[c] {
				widths[c] = w
			}
		}
	}

	if opts.Width > 0 {
		shrinkToFit(widths, opts.Width, nCols)
	}

	align := func(c int) lipgloss.Position {
		if c < len(opts.RightAlign) && opts.RightAlign[c] {
			return lipgloss.Right
		}
		return lipgloss.Left
	}

	// Pre-fit every cell to its final column width: Fit truncates (with an
	// ellipsis) and pads to an exact width, so lipgloss sees fixed-width
	// columns and never wraps. When widths are natural (no shrink) Fit only
	// pads, leaving content intact.
	fitRow := func(cells []string) []string {
		out := make([]string, nCols)
		for c := range nCols {
			cell := ""
			if c < len(cells) {
				cell = cells[c]
			}
			out[c] = Fit(cell, widths[c], align(c))
		}
		return out
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(TUI.TableSeparator()).
		Wrap(false).
		StyleFunc(func(row, _ int) lipgloss.Style {
			st := lipgloss.NewStyle().Padding(0, cellPadding)
			if row == table.HeaderRow {
				return TUI.TableHeader().Padding(0, cellPadding)
			}
			return st
		}).
		Headers(fitRow(headers)...)
	for _, row := range rows {
		t.Row(fitRow(row)...)
	}
	return t.Render()
}

// shrinkToFit reduces column widths in place until the rendered table fits
// within maxWidth, always shaving the currently-widest column (down to
// minColWidth). If every column is already at the floor it stops — the table
// may still overflow, which is preferable to dropping columns entirely.
func shrinkToFit(widths []int, maxWidth, nCols int) {
	for frameWidth(widths, nCols) > maxWidth {
		// Find the widest shrinkable column.
		widest, idx := minColWidth, -1
		for c, w := range widths {
			if w > widest {
				widest, idx = w, c
			}
		}
		if idx == -1 {
			return // nothing left to shrink
		}
		widths[idx]--
	}
}

// frameWidth computes the total rendered width for the given column widths:
// each column contributes its content plus left/right padding, and there is
// one vertical border between every pair of columns plus the two outer edges.
func frameWidth(widths []int, nCols int) int {
	total := nCols + 1 // vertical borders: outer edges + inner separators
	for _, w := range widths {
		total += w + 2*cellPadding
	}
	return total
}
