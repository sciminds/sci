package app

// yank.go — copy the focused cell (y) or row (Y) to the system clipboard,
// plus the preview overlay's y. Platform dispatch (pbcopy / wl-copy /
// xclip / xsel) lives in uikit.Copy — dbtui never shells out itself.
//
// Distinct from visual mode's y/c, which fill the *internal* clipboard
// (m.clipboard) for paste-back; visual's Y/C are the system-clipboard
// multi-row equivalent (see visual.go).

import (
	"fmt"
	"unicode/utf8"

	"github.com/sciminds/sci/internal/uikit"
)

// yankCell copies the focused cell's value to the system clipboard.
//
// Heavy columns hold a server-side placeholder in memory (e.g.
// `<FLOAT[768]>`), so the real payload is refetched through the store's
// CellFetcher — same lazy path the Enter preview takes. A failed fetch has
// already set a status error, so we fall back to the placeholder rather
// than copying nothing.
func (m *Model) yankCell() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	c := m.selectedCell(tab)
	if c == nil {
		return
	}

	if c.Null {
		m.yankText("NULL (empty)", "")
		return
	}

	value := c.Value
	if tab.ColCursor >= 0 && tab.ColCursor < len(tab.Specs) {
		if spec := tab.Specs[tab.ColCursor]; spec.Heavy {
			if fetched, ok := m.fetchHeavyCellValue(tab, spec, tab.Table.Cursor()); ok {
				value = fetched
			}
		}
	}
	m.yankText(cellCopyLabel(value), value)
}

// yankRow copies the focused row to the system clipboard as TSV — the same
// header-plus-data shape visual mode's Y produces, for one row.
func (m *Model) yankRow() {
	tab := m.effectiveTab()
	if tab == nil {
		return
	}
	cursor := tab.Table.Cursor()
	if cursor < 0 || cursor >= len(tab.CellRows) {
		return
	}
	m.yankText(
		fmt.Sprintf("row (%d cols)", len(tab.Specs)),
		formatRowsTSV(tab, []int{cursor}),
	)
}

// yankText is the shared tail of every copy path: write s to the system
// clipboard and report the outcome on the status line. label names what was
// copied ("cell (12 chars)", "row (3 cols)") so callers all speak the same
// wording.
func (m *Model) yankText(label, s string) {
	if err := uikit.Copy(s); err != nil {
		m.setStatusError(fmt.Sprintf("Copy failed: %v", err))
		return
	}
	m.setStatusInfo("Copied " + label)
}

// cellCopyLabel describes a copied cell value for the status line. Length is
// counted in runes, not bytes, so a multi-byte value reports what the user
// sees.
func cellCopyLabel(s string) string {
	if s == "" {
		return "empty cell"
	}
	return fmt.Sprintf("cell (%d chars)", utf8.RuneCountInString(s))
}
