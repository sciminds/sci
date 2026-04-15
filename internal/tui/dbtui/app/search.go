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

// searchMode controls which sources contribute rows to a search.
//
//   - modeDefault: metadata substring only (Zotero-style "All Fields & Tags")
//   - modeFull:    metadata substring ∪ PDF fulltext ∪ note-body substring
//
// Default mode is the quiet default everywhere; Tab toggles into full mode.
// Persisted on Model so switching between modes survives opening/closing the
// search bar within a session (resets on app restart).
type searchMode uint8

const (
	modeDefault searchMode = iota
	modeFull
)

// matchOrigin is a bitset tagging where each displayed row's match came from.
// Cells drive rendering choices: matched metadata cells get green tint on
// their substring positions; when originPDF is set the PDF indicator cell
// gets the tint; when originNote is set the Notes indicator cell gets it.
type matchOrigin uint8

const (
	originMetadata matchOrigin = 1 << iota
	originPDF
	originNote
)

// Column titles used by the origin-cell tint. Matched against
// [columnSpec.Title] — tables without these columns simply don't get
// origin-cell tinting (the metadata-cell tint still works everywhere).
const (
	pdfColumnTitle   = "PDF"
	notesColumnTitle = "Notes"
)

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

	// Origins tags each displayed row's match source (metadata / PDF / note).
	// Used by the renderer to tint the originating cell with a match-tint
	// background. Populated by applySearchFilter.
	Origins map[int]matchOrigin

	// noteHits caches rowIDs whose note body matched the query's tokens.
	// Only populated in modeFull. Applied like ftsHits in applySearchFilter.
	noteHits map[int64]bool

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
// mode selects which sources contribute: modeDefault is metadata-only;
// modeFull adds PDF fulltext (via ftsHits) and note body hits (noteHits).
// ftsHits and noteHits are ignored in modeDefault.
//
// HasPDF on the row metadata gates originPDF so a store bug can't tint a
// PDF cell on a row with no attachment.
func applySearchFilter(
	tab *Tab,
	state *rowSearchState,
	mode searchMode,
	ftsHits map[int64]bool,
	noteHits map[int64]bool,
) {
	if state == nil || state.Query == "" {
		return
	}
	if mode != modeFull {
		ftsHits = nil
		noteHits = nil
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
	origins := make(map[int]matchOrigin)
	var filteredRows []table.Row
	var filteredMeta []rowMeta
	var filteredCells [][]cell
	outIdx := 0

	pdfCol := findColumnIndex(tab, pdfColumnTitle)
	for i, cellRow := range tab.PostPinCellRows {
		meta := tab.PostPinMeta[i]
		mergedHL, metaMatched := matchORGroups(cellRow, resolved)
		ftsHit := ftsHits[meta.RowID]
		noteHit := noteHits[meta.RowID]
		if !metaMatched && !ftsHit && !noteHit {
			continue
		}
		filteredRows = append(filteredRows, tab.PostPinRows[i])
		filteredMeta = append(filteredMeta, meta)
		filteredCells = append(filteredCells, cellRow)
		if len(mergedHL) > 0 {
			highlights[outIdx] = mergedHL
		}
		var origin matchOrigin
		if metaMatched {
			origin |= originMetadata
		}
		if ftsHit && rowHasPDF(cellRow, pdfCol) {
			origin |= originPDF
		}
		if noteHit {
			origin |= originNote
		}
		if origin != 0 {
			origins[outIdx] = origin
		}
		outIdx++
	}

	tab.Rows = filteredMeta
	tab.CellRows = filteredCells
	tab.Table.SetRows(filteredRows)
	state.Highlights = highlights
	state.Origins = origins
}

// buildOriginTint produces a per-row map of viewport-column indices that
// should receive the match-origin tint. Metadata tint follows the
// substring highlight columns; PDF / Notes tint follows the origin bitset
// when the corresponding indicator column exists in the table.
//
// Only cells that actually signal "has content" for PDF/Note indicator
// columns are tinted — a row without a PDF attachment never gets its PDF
// cell tinted, even if the origin bit is somehow set.
func buildOriginTint(tab *Tab, state *rowSearchState, visToFull []int) map[int]map[int]bool {
	if state == nil || len(state.Origins) == 0 {
		return nil
	}
	fullToVP := make(map[int]int, len(visToFull))
	for vp, full := range visToFull {
		fullToVP[full] = vp
	}
	pdfCol := findColumnIndex(tab, pdfColumnTitle)
	notesCol := findColumnIndex(tab, notesColumnTitle)
	pdfVP, pdfInViewport := fullToVP[pdfCol]
	notesVP, notesInViewport := fullToVP[notesCol]

	out := make(map[int]map[int]bool, len(state.Origins))
	for rowIdx, origin := range state.Origins {
		tints := map[int]bool{}
		if origin&originMetadata != 0 {
			for fullCol := range state.Highlights[rowIdx] {
				if vp, ok := fullToVP[fullCol]; ok {
					tints[vp] = true
				}
			}
		}
		if origin&originPDF != 0 && pdfInViewport && rowIdx < len(tab.CellRows) &&
			rowHasPDF(tab.CellRows[rowIdx], pdfCol) {
			tints[pdfVP] = true
		}
		if origin&originNote != 0 && notesInViewport && rowIdx < len(tab.CellRows) &&
			rowHasNote(tab.CellRows[rowIdx], notesCol) {
			tints[notesVP] = true
		}
		if len(tints) > 0 {
			out[rowIdx] = tints
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// findColumnIndex returns the full-column index of the first column whose
// Title matches want, or -1 if none. Used to locate PDF/Notes indicator
// columns for origin tinting — tables without them get no origin tint,
// which is the intended graceful degradation.
func findColumnIndex(tab *Tab, want string) int {
	for i, spec := range tab.Specs {
		if spec.Title == want {
			return i
		}
	}
	return -1
}

// rowHasPDF reports whether the given cell row's PDF-indicator cell is a
// "has PDF" marker. Guards originPDF so a store bug can't paint the PDF
// cell green on a row with no attachment.
func rowHasPDF(row []cell, pdfCol int) bool {
	if pdfCol < 0 || pdfCol >= len(row) {
		return false
	}
	v := strings.TrimSpace(row[pdfCol].Value)
	return v != "" && v != "-"
}

// rowHasNote reports whether the given cell row's Notes-indicator cell is
// a "has note" marker. Guards originNote rendering for the same reason.
func rowHasNote(row []cell, notesCol int) bool {
	if notesCol < 0 || notesCol >= len(row) {
		return false
	}
	v := strings.TrimSpace(row[notesCol].Value)
	return v != "" && v != "-"
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

	// Immediate filter: use whatever FTS + note hits are cached (only in full mode).
	m.reapplySearchFilter(tab)
	tab.InvalidateVP()
	tab.Table.SetCursor(clampCursor(tab.Table.Cursor(), len(tab.CellRows)))

	// Schedule a debounced FTS query only in full mode.
	if m.searchMode != modeFull {
		return nil
	}
	if _, ok := m.store.(data.FulltextSearcher); !ok {
		return nil
	}
	m.search.ftsSeq++
	seq := m.search.ftsSeq
	return tea.Tick(ftsDebounce, func(_ time.Time) tea.Msg {
		return ftsTickMsg{Seq: seq}
	})
}

// reapplySearchFilter re-runs applySearchFilter using the model's current
// searchMode and the state's cached FTS / note hits. Single-call-site helper
// so mode and caches stay in sync across the hot paths.
func (m *Model) reapplySearchFilter(tab *Tab) {
	// Populate note hits lazily on every re-filter — cheap substring scan
	// over the pre-lowered bodies. Skipping this when mode=default saves the
	// work entirely.
	var noteHits map[int64]bool
	if m.searchMode == modeFull {
		noteHits = m.buildNoteHits(tab)
		m.search.noteHits = noteHits
	} else {
		m.search.noteHits = nil
	}
	applySearchFilter(tab, m.search, m.searchMode, m.search.ftsHits, noteHits)
}

// buildNoteHits returns the set of rowIDs whose docling-extracted note body
// matches the current query's tokens (AND across tokens, case-insensitive).
// Returns nil when the store doesn't expose note bodies or the query has no
// usable terms. Runs synchronously: the pre-lowered cache keeps this to
// O(rows × tokens) cheap substring scans per keystroke.
func (m *Model) buildNoteHits(tab *Tab) map[int64]bool {
	provider, ok := m.store.(data.NoteBodyProvider)
	if !ok {
		return nil
	}
	groups := match.ParseClauses(m.search.Query)
	// Collect all unscoped, non-negated tokens across all groups —
	// note body search mirrors FTS scoping semantics.
	tokens := lo.FlatMap(groups, func(group []match.Clause, _ int) []string {
		return lo.FlatMap(group, func(c match.Clause, _ int) []string {
			if c.Column != "" || c.Negate || c.Terms == "" {
				return nil
			}
			// Strip quotes if quoted — note search is substring, not FTS-exact.
			fields := strings.Fields(c.Terms)
			return lo.Map(fields, func(f string, _ int) string {
				if len(f) >= 2 && f[0] == '"' && f[len(f)-1] == '"' {
					return strings.ToLower(f[1 : len(f)-1])
				}
				return strings.ToLower(f)
			})
		})
	})
	if len(tokens) == 0 {
		return nil
	}
	hits := map[int64]bool{}
	for _, meta := range tab.PostPinMeta {
		lower := provider.NoteBody(tab.Name, meta.RowID)
		if lower == "" {
			continue
		}
		allMatch := true
		for _, tok := range tokens {
			if !strings.Contains(lower, tok) {
				allMatch = false
				break
			}
		}
		if allMatch {
			hits[meta.RowID] = true
		}
	}
	if len(hits) == 0 {
		return nil
	}
	return hits
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

	m.reapplySearchFilter(tab)
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
	case keyTab:
		m.toggleSearchMode()
		return m.rerunSearch()
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

// toggleSearchMode flips between default (metadata only) and full (FTS +
// notes) search modes. Cached FTS hits are dropped on switch: in full mode
// they'll repopulate from the debounced tick; in default mode they should
// no longer filter the row set.
func (m *Model) toggleSearchMode() {
	if m.searchMode == modeFull {
		m.searchMode = modeDefault
	} else {
		m.searchMode = modeFull
	}
	if m.search != nil {
		m.search.ftsHits = nil
		m.search.ftsLoading = false
	}
}

// renderSearchBar renders the inline search bar above the table header.
func (m *Model) renderSearchBar() string {
	if m.search == nil {
		return ""
	}
	s := m.search

	// Full mode: green prompt glyph and a ★ marker so the mode is obvious
	// even if the Tab hint wraps off the right edge.
	var prompt string
	if m.searchMode == modeFull {
		prompt = m.styles.Keycap().Background(m.styles.Palette().Green).Render("/\u2605")
	} else {
		prompt = m.styles.Keycap().Render("/")
	}
	cursor := m.styles.HeaderHint().Render("\u2502")
	queryText := s.Query + cursor
	if s.Query == "" {
		placeholder := "search, @col: val, -exclude, | or"
		queryText = m.styles.Empty().Render(placeholder) + cursor
	}

	// Mode hint — always shown so the user learns the Tab toggle exists.
	var modeHint string
	if m.searchMode == modeFull {
		modeHint = m.styles.HeaderHint().Render(" Tab: default")
	} else {
		modeHint = m.styles.HeaderHint().Render(" Tab: full-text")
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

	left := prompt + " " + queryText + modeHint
	right := countLabel

	return uikit.SpreadMinGap(m.width, 1, left, right)
}
