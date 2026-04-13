package app

// search.go — incremental search with highlight: text input, match navigation
// (next/prev), and cell highlight rendering across visible rows.
//
// Matching algorithms and query parsing live in [match].

import (
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/tui/uikit"
)

// rowSearchState holds the state for the inline row search bar.
type rowSearchState struct {
	Query     string
	Column    string // scoped column name from @col syntax; empty = all columns
	Committed bool   // true after Enter — bar hidden, filter + highlights kept

	// Highlights stores per-row, per-column match positions for rendering.
	// Key: displayed row index → column index → rune positions.
	Highlights map[int]map[int][]int
}

// resolvedClause is a parsed clause with its column index resolved.
type resolvedClause struct {
	terms     string
	scopedCol int // -1 = all columns
	negate    bool
}

// resolvedGroup is one OR branch: a slice of AND-clauses.
type resolvedGroup []resolvedClause

// applySearchFilter filters the post-pin data using the search query.
// It reads from tab.PostPin* and writes matching rows to tab.CellRows/Rows/Table.
// state.Highlights is populated with per-cell match positions for rendering.
//
// The query supports OR groups (separated by " | "), AND clauses (multiple @col
// terms within a group), and negation ("-" prefix excludes matches).
// A row matches if ANY OR group matches. Within a group, ALL AND clauses must match.
func applySearchFilter(tab *Tab, state *rowSearchState) {
	if state == nil || state.Query == "" {
		return
	}

	groups := match.ParseClauses(state.Query)
	if len(groups) == 0 {
		return
	}

	// Merge in the Column field from state if the first group's first clause
	// has none (backward compat with programmatic Column setting).
	if len(groups) == 1 && len(groups[0]) == 1 && groups[0][0].Column == "" && state.Column != "" {
		groups[0][0].Column = state.Column
	}

	// Resolve column indices for each clause across all groups.
	resolved, ok := resolveGroups(groups, tab)
	if !ok {
		// An invalid column was referenced — no rows match.
		tab.Rows = nil
		tab.CellRows = nil
		tab.Table.SetRows(nil)
		return
	}
	if len(resolved) == 0 {
		return
	}

	highlights := make(map[int]map[int][]int)
	var filteredRows []table.Row
	var filteredMeta []rowMeta
	var filteredCells [][]cell
	outIdx := 0

	for i, cellRow := range tab.PostPinCellRows {
		mergedHL, matched := matchORGroups(cellRow, resolved)
		if !matched {
			continue
		}
		filteredRows = append(filteredRows, tab.PostPinRows[i])
		filteredMeta = append(filteredMeta, tab.PostPinMeta[i])
		filteredCells = append(filteredCells, cellRow)
		if len(mergedHL) > 0 {
			highlights[outIdx] = mergedHL
		}
		outIdx++
	}

	tab.Rows = filteredMeta
	tab.CellRows = filteredCells
	tab.Table.SetRows(filteredRows)
	state.Highlights = highlights
}

// resolveGroups converts parsed OR groups into resolved groups with column indices.
// Returns (nil, false) if any clause references an invalid column.
func resolveGroups(groups [][]match.Clause, tab *Tab) ([]resolvedGroup, bool) {
	var out []resolvedGroup
	for _, group := range groups {
		var rg resolvedGroup
		for _, c := range group {
			if c.Terms == "" {
				continue
			}
			sc := -1
			if c.Column != "" {
				_, idx, found := lo.FindIndexOf(tab.Specs, func(spec columnSpec) bool {
					return strings.EqualFold(spec.Title, c.Column) || strings.EqualFold(spec.DBName, c.Column)
				})
				if !found {
					return nil, false
				}
				sc = idx
			}
			rg = append(rg, resolvedClause{terms: c.Terms, scopedCol: sc, negate: c.Negate})
		}
		if len(rg) > 0 {
			out = append(out, rg)
		}
	}
	return out, true
}

// matchORGroups checks if any OR group matches the row.
// Returns merged highlights and whether the row matched.
func matchORGroups(row []cell, groups []resolvedGroup) (map[int][]int, bool) {
	for _, group := range groups {
		mergedHL, ok := matchANDGroup(row, group)
		if ok {
			return mergedHL, true
		}
	}
	return nil, false
}

// matchANDGroup checks if all AND clauses in a group match the row.
func matchANDGroup(row []cell, group resolvedGroup) (map[int][]int, bool) {
	mergedHL := map[int][]int{}
	for _, rc := range group {
		rowHL, matched := matchRow(row, rc.terms, rc.scopedCol)
		if rc.negate {
			// Negated: row must NOT match.
			if matched {
				return nil, false
			}
			// No highlights for negated clauses — they exclude, not highlight.
			continue
		}
		if !matched {
			return nil, false
		}
		maps.Copy(mergedHL, rowHL)
	}
	return mergedHL, true
}

// matchRow checks if any cell in the row matches the search terms.
// Returns per-column highlight positions and whether the row matched.
func matchRow(row []cell, terms string, scopedCol int) (map[int][]int, bool) {
	highlights := map[int][]int{}
	matched := false

	for i, c := range row {
		if scopedCol >= 0 && i != scopedCol {
			continue
		}
		value := firstLine(c.Value)
		score, positions := match.Fuzzy(terms, value)
		if score > 0 && len(positions) > 0 {
			highlights[i] = positions
			matched = true
		}
	}
	return highlights, matched
}

// openSearch activates the inline search bar.
// If a committed search exists, reopens it for editing.
func (m *Model) openSearch() {
	// Ensure PostPin snapshot exists so search has data to filter.
	if tab := m.effectiveTab(); tab != nil && tab.PostPinCellRows == nil {
		tabstate.SnapshotPostPin(tab)
	}
	if m.search != nil && m.search.Committed {
		m.search.Committed = false
		m.resizeTables()
		return
	}
	m.search = &rowSearchState{}
}

// closeSearch deactivates the search bar and restores post-pin data.
func (m *Model) closeSearch() {
	m.search = nil
	if tab := m.effectiveTab(); tab != nil {
		// Restore from PostPin snapshot.
		tab.Rows = tabstate.CopyMeta(tab.PostPinMeta)
		tab.CellRows = tab.PostPinCellRows
		tab.Table.SetRows(tab.PostPinRows)
		tab.InvalidateVP()
	}
	m.resizeTables()
}

// rerunSearch re-applies the search filter on the current tab.
func (m *Model) rerunSearch() {
	tab := m.effectiveTab()
	if tab == nil || m.search == nil {
		return
	}
	// Restore PostPin data before re-filtering.
	tab.Rows = tabstate.CopyMeta(tab.PostPinMeta)
	tab.CellRows = tab.PostPinCellRows
	tab.Table.SetRows(tab.PostPinRows)

	applySearchFilter(tab, m.search)
	tab.InvalidateVP()

	tab.Table.SetCursor(clampCursor(tab.Table.Cursor(), len(tab.CellRows)))
}

// handleSearchKey processes keystrokes while the search bar is active.
func (m *Model) handleSearchKey(key tea.KeyPressMsg) tea.Cmd {
	k := key.String()

	switch k {
	case keyEsc:
		m.closeSearch()
		return nil
	case keyEnter:
		// Hide search bar but keep filter + highlights active.
		m.search.Committed = true
		m.resizeTables()
		return nil
	case keyUp, keyCtrlP:
		if tab := m.effectiveTab(); tab != nil {
			m.cursorUp(tab)
		}
		return nil
	case keyDown, keyCtrlN:
		if tab := m.effectiveTab(); tab != nil {
			m.cursorDown(tab)
		}
		return nil
	case keyBackspace:
		if len(m.search.Query) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.search.Query)
			m.search.Query = m.search.Query[:len(m.search.Query)-size]
			m.rerunSearch()
		}
		return nil
	default:
		if key.Text != "" {
			m.search.Query += key.Text
			m.rerunSearch()
		}
		return nil
	}
}

// renderSearchBar renders the inline search bar above the table header.
func (m *Model) renderSearchBar() string {
	if m.search == nil {
		return ""
	}
	s := m.search

	prompt := m.styles.Keycap().Render("/")
	cursor := m.styles.HeaderHint().Render("\u2502")
	queryText := s.Query + cursor
	if s.Query == "" {
		queryText = m.styles.Empty().Render("search, @col: val, -exclude, | or") + cursor
	}

	// Match count.
	var countLabel string
	if tab := m.effectiveTab(); tab != nil && s.Query != "" {
		total := len(tab.PostPinCellRows)
		matched := len(tab.CellRows)
		countLabel = m.styles.HeaderHint().Render(fmt.Sprintf(" %d/%d", matched, total))
	}

	left := prompt + " " + queryText
	right := countLabel

	return uikit.SpreadMinGap(m.width, 1, left, right)
}
