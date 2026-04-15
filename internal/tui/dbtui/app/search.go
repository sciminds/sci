package app

// search.go — incremental search with highlight: text input, match navigation
// (next/prev), and cell highlight rendering across visible rows.
//
// Matching algorithms and query parsing live in [match].

import (
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
	"github.com/sciminds/cli/internal/uikit"
)

// ftsDebounce is the delay before firing the fulltext search query after
// the user stops typing. Short enough to feel responsive, long enough to
// avoid hammering the DB on every keystroke.
const ftsDebounce = 200 * time.Millisecond

// ftsTickMsg is sent by a debounced tea.Tick to trigger a fulltext search.
// The Seq field is compared to rowSearchState.ftsSeq — stale ticks (where
// the user kept typing) are ignored.
type ftsTickMsg struct{ Seq uint }

// ftsResultMsg carries the result of an async fulltext search query back to
// the main Update loop. Seq is checked against rowSearchState.ftsSeq to
// discard stale results (the user kept typing while the query was in flight).
type ftsResultMsg struct {
	Seq  uint
	Hits map[int64]bool
}

// rowSearchState holds the state for the inline row search bar.
type rowSearchState struct {
	Query     string
	Column    string // scoped column name from @col syntax; empty = all columns
	Committed bool   // true after Enter — bar hidden, filter + highlights kept

	// Highlights stores per-row, per-column match positions for rendering.
	// Key: displayed row index → column index → rune positions.
	Highlights map[int]map[int][]int

	// ftsSeq is incremented on every keystroke that changes the query.
	// A ftsTickMsg whose Seq matches triggers the (expensive) FTS query.
	ftsSeq uint
	// ftsHits caches the most recent fulltext search results so the
	// immediate fuzzy re-filter can union them without waiting for the
	// debounced FTS query.
	ftsHits map[int64]bool
	// ftsLoading is true while an async FTS query is in flight.
	// The search bar shows a spinner instead of the match count.
	ftsLoading bool
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
//
// ftsHits is an optional pre-computed set of rowIDs from the fulltext index.
// When non-nil, rows matching either fuzzy columns or the FTS set are kept.
// Pass nil to skip fulltext matching entirely (e.g. during the immediate
// keystroke filter before the debounced FTS query fires).
func applySearchFilter(tab *Tab, state *rowSearchState, ftsHits map[int64]bool) {
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
		// A row matches if fuzzy column search hit OR fulltext content hit.
		if !matched && !ftsHits[tab.PostPinMeta[i].RowID] {
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

// buildFTSHitSet queries the store's fulltext index for unscoped, non-negated
// terms. Returns a set of matching rowIDs, or nil if FTS is not available or
// the query has no eligible terms.
//
// Token semantics:
//   - "word" (double-quoted) → exact word match, always.
//   - single unquoted token → prefix match ("neuro" → "neuroimaging"), so
//     incremental typing feels responsive.
//   - two or more unquoted tokens → exact word match. Once the user has
//     typed multiple words the intent is no longer incremental, and prefix
//     expansion on every token produces far too many false positives
//     (e.g. "drives" prefix also matches "drove/driver/driven" in unrelated
//     PDFs that happen to contain the other token).
func buildFTSHitSet(groups [][]match.Clause, store data.DataStore, table string) map[int64]bool {
	if store == nil {
		return nil
	}
	fts, ok := store.(data.FulltextSearcher)
	if !ok {
		return nil
	}

	// Collect all unscoped, non-negated terms across all groups.
	allWords := lo.FlatMap(groups, func(group []match.Clause, _ int) []string {
		return lo.FlatMap(group, func(c match.Clause, _ int) []string {
			if c.Column != "" || c.Negate || c.Terms == "" {
				return nil
			}
			return strings.Fields(c.Terms)
		})
	})
	if len(allWords) == 0 {
		return nil
	}

	isQuoted := func(w string) bool {
		return len(w) >= 2 && w[0] == '"' && w[len(w)-1] == '"'
	}
	unquote := func(w string) string { return w[1 : len(w)-1] }

	quotedWords := lo.FilterMap(allWords, func(w string, _ int) (string, bool) {
		if !isQuoted(w) {
			return "", false
		}
		return unquote(w), true
	})
	rawUnquoted := lo.Filter(allWords, func(w string, _ int) bool { return !isQuoted(w) })

	// Multi-token queries force unquoted words to exact match; a lone
	// unquoted word stays as a prefix for incremental typing.
	var prefixWords, exactWords []string
	exactWords = append(exactWords, quotedWords...)
	if len(allWords) > 1 {
		exactWords = append(exactWords, rawUnquoted...)
	} else {
		prefixWords = rawUnquoted
	}

	idsToBoolSet := func(ids []int64) map[int64]bool {
		return lo.SliceToMap(ids, func(id int64) (int64, bool) { return id, true })
	}
	var result map[int64]bool

	if len(prefixWords) > 0 {
		if ids, err := fts.SearchFulltext(table, prefixWords, false); err == nil && len(ids) > 0 {
			result = idsToBoolSet(ids)
		}
	}
	if len(exactWords) > 0 {
		if ids, err := fts.SearchFulltext(table, exactWords, true); err == nil && len(ids) > 0 {
			if result == nil {
				result = idsToBoolSet(ids)
			} else {
				exactSet := idsToBoolSet(ids)
				result = lo.PickBy(result, func(id int64, _ bool) bool {
					return exactSet[id]
				})
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
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

// matchRow checks if the row satisfies the search terms under token-AND
// semantics: the terms are split on whitespace and each token must appear
// (case-insensitive substring) in some cell of the row. scopedCol restricts
// matching to a single column when >= 0. Returns per-column rune-index
// highlight positions and whether the row matched.
func matchRow(row []cell, terms string, scopedCol int) (map[int][]int, bool) {
	tokens := strings.Fields(terms)
	cells := lo.Map(row, func(c cell, _ int) string { return firstLine(c.Value) })
	return match.MatchRow(tokens, cells, scopedCol)
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
// It runs the fast in-memory fuzzy filter immediately (using cached FTS hits)
// and returns a debounced tea.Cmd that will fire the expensive fulltext query
// after the user stops typing.
func (m *Model) rerunSearch() tea.Cmd {
	tab := m.effectiveTab()
	if tab == nil || m.search == nil {
		return nil
	}
	// Restore PostPin data before re-filtering.
	tab.Rows = tabstate.CopyMeta(tab.PostPinMeta)
	tab.CellRows = tab.PostPinCellRows
	tab.Table.SetRows(tab.PostPinRows)

	// Immediate filter: fuzzy column matching + whatever FTS hits are cached.
	applySearchFilter(tab, m.search, m.search.ftsHits)
	tab.InvalidateVP()
	tab.Table.SetCursor(clampCursor(tab.Table.Cursor(), len(tab.CellRows)))

	// Schedule a debounced FTS query if the store supports fulltext search.
	if _, ok := m.store.(data.FulltextSearcher); !ok {
		return nil
	}
	m.search.ftsSeq++
	seq := m.search.ftsSeq
	return tea.Tick(ftsDebounce, func(_ time.Time) tea.Msg {
		return ftsTickMsg{Seq: seq}
	})
}

// handleFTSTick processes a debounced fulltext search tick. If the sequence
// matches (the user stopped typing), it sets ftsLoading and returns a tea.Cmd
// that runs the expensive FTS query off the main goroutine. The result arrives
// as an ftsResultMsg, processed by handleFTSResult.
func (m *Model) handleFTSTick(msg ftsTickMsg) tea.Cmd {
	if m.search == nil || msg.Seq != m.search.ftsSeq {
		return nil // stale tick — user kept typing
	}
	tab := m.effectiveTab()
	if tab == nil {
		return nil
	}

	m.search.ftsLoading = true
	query := m.search.Query
	store := m.store
	name := tab.Name
	seq := msg.Seq
	return func() tea.Msg {
		groups := match.ParseClauses(query)
		hits := buildFTSHitSet(groups, store, name)
		return ftsResultMsg{Seq: seq, Hits: hits}
	}
}

// handleFTSResult processes the result of an async fulltext search. If the
// sequence matches, it caches the hits, clears ftsLoading, and re-filters
// the tab with the full hit set.
func (m *Model) handleFTSResult(msg ftsResultMsg) {
	if m.search == nil || msg.Seq != m.search.ftsSeq {
		return // stale result — user kept typing while query was in flight
	}
	tab := m.effectiveTab()
	if tab == nil {
		return
	}

	m.search.ftsLoading = false
	m.search.ftsHits = msg.Hits

	// Re-filter with the fresh FTS results.
	tab.Rows = tabstate.CopyMeta(tab.PostPinMeta)
	tab.CellRows = tab.PostPinCellRows
	tab.Table.SetRows(tab.PostPinRows)

	applySearchFilter(tab, m.search, msg.Hits)
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
			return m.rerunSearch()
		}
		return nil
	default:
		if key.Text != "" {
			m.search.Query += key.Text
			return m.rerunSearch()
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
		placeholder := "search, @col: val, -exclude, | or"
		if _, ok := m.store.(data.FulltextSearcher); ok {
			placeholder = "search (+ PDF content), @col: val, -exclude, | or"
		}
		queryText = m.styles.Empty().Render(placeholder) + cursor
	}

	// Match count — or spinner when an FTS query is in flight.
	var countLabel string
	if s.ftsLoading {
		countLabel = " " + m.spinner.View() + " " + m.styles.HeaderHint().Render("searching…")
	} else if tab := m.effectiveTab(); tab != nil && s.Query != "" {
		total := len(tab.PostPinCellRows)
		matched := len(tab.CellRows)
		countLabel = m.styles.HeaderHint().Render(fmt.Sprintf(" %d/%d", matched, total))
	}

	left := prompt + " " + queryText
	right := countLabel

	return uikit.SpreadMinGap(m.width, 1, left, right)
}
