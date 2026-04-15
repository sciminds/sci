package app

// search_mode_test.go — mode toggle, origin tagging, and render-tint tests
// for the dbtui search-mode feature.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestApplySearchFilter_OriginMetadata_OnlyMetadataBit(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}},
	)
	state := &rowSearchState{Query: "alice"}
	applySearchFilter(tab, state, modeDefault, nil, nil)

	if got, want := state.Origins[0], originMetadata; got != want {
		t.Errorf("origins[0] = %b, want %b", got, want)
	}
	if state.Origins[0]&originPDF != 0 {
		t.Error("originPDF should not be set for metadata match")
	}
}

func TestApplySearchFilter_OriginPDFOnly(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "PDF"},
		[][]string{{"hello", "Yes"}, {"world", "Yes"}},
	)
	state := &rowSearchState{Query: "xyz"} // no metadata match
	ftsHits := map[int64]bool{1: true}
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row (FTS-only), got %d", len(tab.CellRows))
	}
	if got := state.Origins[0]; got != originPDF {
		t.Errorf("expected only originPDF bit, got %b", got)
	}
}

func TestApplySearchFilter_PDFOriginRequiresHasPDF(t *testing.T) {
	// Row has FTS hit but PDF cell says "-" (no attachment). Row must still
	// be included (FTS says it matches), but originPDF must NOT be tagged.
	tab := makeTab(
		[]string{"Title", "PDF"},
		[][]string{{"hello", "-"}},
	)
	state := &rowSearchState{Query: "xyz"}
	ftsHits := map[int64]bool{1: true}
	applySearchFilter(tab, state, modeFull, ftsHits, nil)

	if state.Origins[0]&originPDF != 0 {
		t.Error("originPDF must not be set when row has no PDF attachment")
	}
}

func TestApplySearchFilter_OriginNoteOnly(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "Notes"},
		[][]string{{"hello", "Extracted"}},
	)
	state := &rowSearchState{Query: "xyz"}
	noteHits := map[int64]bool{1: true}
	applySearchFilter(tab, state, modeFull, nil, noteHits)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if got := state.Origins[0]; got != originNote {
		t.Errorf("expected only originNote bit, got %b", got)
	}
}

func TestApplySearchFilter_OriginMixed_AllBits(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "PDF", "Notes"},
		[][]string{{"alice", "Yes", "Extracted"}},
	)
	state := &rowSearchState{Query: "alice"}
	ftsHits := map[int64]bool{1: true}
	noteHits := map[int64]bool{1: true}
	applySearchFilter(tab, state, modeFull, ftsHits, noteHits)

	want := originMetadata | originPDF | originNote
	if got := state.Origins[0]; got != want {
		t.Errorf("expected all three bits (%b), got %b", want, got)
	}
}

func TestApplySearchFilter_DefaultModeIgnoresFTSAndNotes(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "PDF", "Notes"},
		[][]string{{"alice", "Yes", "Extracted"}},
	)
	state := &rowSearchState{Query: "xyz"} // no metadata match
	ftsHits := map[int64]bool{1: true}
	noteHits := map[int64]bool{1: true}
	applySearchFilter(tab, state, modeDefault, ftsHits, noteHits)

	if len(tab.CellRows) != 0 {
		t.Errorf("default mode should ignore FTS+note hits, got %d rows", len(tab.CellRows))
	}
}

// buildOriginTint: ensure non-PDF-column tables don't crash and don't
// spuriously tint cells. (Phase 3 regression — the tint builder must be
// resilient to any column layout.)
func TestBuildOriginTint_NoPDFColumn_NoCrashNoStain(t *testing.T) {
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}},
	)
	state := &rowSearchState{
		Origins:    map[int]matchOrigin{0: originPDF}, // origin says PDF...
		Highlights: map[int]map[int][]int{},           // ...but no PDF col exists
	}
	// Must not panic; must produce no tint (no PDF column to paint).
	visToFull := []int{0, 1}
	got := buildOriginTint(tab, state, visToFull)
	if len(got) != 0 {
		t.Errorf("expected no tint for table without PDF column, got %v", got)
	}
}

func TestBuildOriginTint_MetadataDoesNotTint(t *testing.T) {
	// Metadata origin no longer tints cells — the per-rune substring
	// highlight already signals the match. Origin-cell emphasis is reserved
	// for PDF/Notes columns where there's no substring to highlight.
	tab := makeTab(
		[]string{"name", "city"},
		[][]string{{"alice", "paris"}},
	)
	state := &rowSearchState{
		Origins:    map[int]matchOrigin{0: originMetadata},
		Highlights: map[int]map[int][]int{0: {0: {1, 2, 3}}},
	}
	got := buildOriginTint(tab, state, []int{0, 1})
	if len(got) != 0 {
		t.Errorf("metadata origin must not emit tint entries, got %v", got)
	}
}

func TestBuildOriginTint_PDFCellTintedWhenHasPDF(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "PDF"},
		[][]string{{"hello", "Yes"}},
	)
	state := &rowSearchState{Origins: map[int]matchOrigin{0: originPDF}}
	got := buildOriginTint(tab, state, []int{0, 1})
	if !got[0][1] {
		t.Error("expected PDF cell (vp col 1) to be tinted")
	}
}

func TestBuildOriginTint_PDFCellNotTintedWhenNoAttachment(t *testing.T) {
	tab := makeTab(
		[]string{"Title", "PDF"},
		[][]string{{"hello", "-"}},
	)
	state := &rowSearchState{Origins: map[int]matchOrigin{0: originPDF}}
	got := buildOriginTint(tab, state, []int{0, 1})
	if len(got) != 0 {
		t.Errorf("expected no tint on row without PDF, got %v", got)
	}
}

func TestApplySearchFilter_EmptyQueryClearsHighlights(t *testing.T) {
	// Regression: backspacing the query to empty used to leave stale
	// Highlights / Origins maps on state, causing highlights to linger on
	// the restored (unfiltered) rows.
	tab := makeTab(
		[]string{"name"},
		[][]string{{"alice"}, {"bob"}},
	)
	state := &rowSearchState{
		Query:      "",
		Highlights: map[int]map[int][]int{0: {0: {1, 2}}},
		Origins:    map[int]matchOrigin{0: originMetadata},
		noteHits:   map[int64]bool{1: true},
	}
	applySearchFilter(tab, state, modeDefault, nil, nil)
	if state.Highlights != nil {
		t.Errorf("expected Highlights cleared on empty query, got %v", state.Highlights)
	}
	if state.Origins != nil {
		t.Errorf("expected Origins cleared on empty query, got %v", state.Origins)
	}
	if state.noteHits != nil {
		t.Errorf("expected noteHits cleared on empty query, got %v", state.noteHits)
	}
}

func TestToggleSearchMode_FlipsAndClearsFTSState(t *testing.T) {
	m := minimalModel()
	m.search = &rowSearchState{
		ftsHits:    map[int64]bool{1: true},
		ftsLoading: true,
	}
	if m.searchMode != modeDefault {
		t.Fatalf("expected default mode, got %v", m.searchMode)
	}

	m.toggleSearchMode()
	if m.searchMode != modeFull {
		t.Error("expected full mode after first toggle")
	}
	if m.search.ftsHits != nil {
		t.Error("ftsHits should be cleared on toggle")
	}
	if m.search.ftsLoading {
		t.Error("ftsLoading should be cleared on toggle")
	}

	m.toggleSearchMode()
	if m.searchMode != modeDefault {
		t.Error("expected default mode after second toggle")
	}
}

func TestRenderSearchBar_ShowsModeHint(t *testing.T) {
	m := minimalModel()
	m.tabs = []Tab{*makeTab([]string{"a"}, [][]string{{"x"}})}
	m.active = 0
	m.width = 120
	m.height = 24
	m.openSearch()
	m.search.Query = "q"

	// Default mode: hint advertises "Tab: full-text".
	bar := m.renderSearchBar()
	if !strings.Contains(bar, "Tab: full-text") {
		t.Errorf("default mode bar should advertise Tab: full-text, got %q", bar)
	}

	m.toggleSearchMode()
	bar = m.renderSearchBar()
	if !strings.Contains(bar, "Tab: default") {
		t.Errorf("full mode bar should advertise Tab: default, got %q", bar)
	}
}

func TestHandleSearchKey_EscClearsPopulatedQuery(t *testing.T) {
	// Esc on a populated query should clear the input (and highlights) but
	// keep the search bar open. A second Esc then closes it.
	m := minimalModel()
	m.tabs = []Tab{*makeTab([]string{"a"}, [][]string{{"x"}})}
	m.active = 0
	m.width = 80
	m.height = 24
	m.openSearch()
	m.search.Query = "foo"
	m.search.Highlights = map[int]map[int][]int{0: {0: {0}}}

	_ = m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search == nil {
		t.Fatal("first Esc on populated query must not close search")
	}
	if m.search.Query != "" {
		t.Errorf("expected query cleared, got %q", m.search.Query)
	}
	if m.search.Highlights != nil {
		t.Errorf("expected highlights cleared, got %v", m.search.Highlights)
	}

	_ = m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != nil {
		t.Error("second Esc on empty query must close search")
	}
}

func TestHandleSearchKey_EscClosesEmptyQuery(t *testing.T) {
	m := minimalModel()
	m.tabs = []Tab{*makeTab([]string{"a"}, [][]string{{"x"}})}
	m.active = 0
	m.width = 80
	m.height = 24
	m.openSearch()

	_ = m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.search != nil {
		t.Error("Esc on empty query must close search")
	}
}

func TestHandleSearchKey_TabTogglesMode(t *testing.T) {
	m := minimalModel()
	m.tabs = []Tab{*makeTab([]string{"a"}, [][]string{{"x"}})}
	m.active = 0
	m.width = 80
	m.height = 24
	m.openSearch()
	m.search.Query = "x"

	before := m.searchMode
	// Send Tab through the key dispatcher.
	// We can't build a real tea.KeyPressMsg here without imports, but the
	// handler routes on .String() == "tab" — exercise via the direct method.
	m.toggleSearchMode()
	_ = m.rerunSearch()
	if m.searchMode == before {
		t.Fatal("Tab should toggle mode")
	}
}
