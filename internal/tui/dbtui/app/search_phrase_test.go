package app

// search_phrase_test.go — quoted-phrase search support across the three
// search surfaces: metadata cells, docling note bodies, and PDF fulltext.

import (
	"slices"
	"testing"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/data"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
	"github.com/sciminds/cli/internal/tui/dbtui/tabstate"
)

func TestApplySearchFilter_Phrase_MetadataContiguousOnly(t *testing.T) {
	tab := makeTab(
		[]string{"title"},
		[][]string{
			{"gossip drives deposition"},
			{"gossip about drives"},
		},
	)
	state := &rowSearchState{Query: `"gossip drives"`}
	applySearchFilter(tab, state, modeDefault, nil, nil)

	if len(tab.CellRows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(tab.CellRows))
	}
	if v := tab.CellRows[0][0].Value; v != "gossip drives deposition" {
		t.Errorf("matched row = %q; want the contiguous-phrase row", v)
	}
}

func TestApplySearchFilter_UnquotedMultiTokenStillANDs(t *testing.T) {
	// Regression: bare `gossip drives` must still AND-match scattered words.
	tab := makeTab(
		[]string{"title"},
		[][]string{
			{"gossip drives deposition"},
			{"gossip about drives"},
		},
	)
	state := &rowSearchState{Query: "gossip drives"}
	applySearchFilter(tab, state, modeDefault, nil, nil)

	if len(tab.CellRows) != 2 {
		t.Fatalf("unquoted tokens should match both rows, got %d", len(tab.CellRows))
	}
}

// mockNoteProvider implements data.NoteBodyProvider with a fixed map of
// pre-lowered bodies keyed by rowID.
type mockNoteProvider struct {
	data.DataStore
	bodies map[int64]string
}

func (m *mockNoteProvider) NoteBody(_ string, rowID int64) string {
	return m.bodies[rowID]
}

func TestBuildNoteHits_Phrase_ContiguousOnly(t *testing.T) {
	// Note body 1 has phrase contiguous; body 2 has the words scattered.
	m := minimalModel()
	m.store = &mockNoteProvider{bodies: map[int64]string{
		1: "transformer models are the gossip drives of nlp",
		2: "gossip about the drives of transformers",
	}}
	tab := makeTab([]string{"x"}, [][]string{{"a"}, {"b"}})
	tab.PostPinMeta = []rowMeta{{RowID: 1}, {RowID: 2}}
	m.tabs = []Tab{*tab}
	m.active = 0
	m.searchMode = modeFull
	m.search = &rowSearchState{Query: `"gossip drives"`}

	hits := m.buildNoteHits(&m.tabs[0])
	if !hits[1] {
		t.Error("rowID 1 (contiguous phrase in body) should hit")
	}
	if hits[2] {
		t.Error("rowID 2 (scattered words in body) must not hit — phrase requires adjacency")
	}
}

func TestBuildNoteHits_UnquotedStillANDs(t *testing.T) {
	m := minimalModel()
	m.store = &mockNoteProvider{bodies: map[int64]string{
		1: "gossip about the drives of transformers",
	}}
	tab := makeTab([]string{"x"}, [][]string{{"a"}})
	tab.PostPinMeta = []rowMeta{{RowID: 1}}
	m.tabs = []Tab{*tab}
	m.active = 0
	m.searchMode = modeFull
	m.search = &rowSearchState{Query: "gossip drives"}

	hits := m.buildNoteHits(&m.tabs[0])
	if !hits[1] {
		t.Error("unquoted multi-token should AND-match scattered words in note body")
	}
}

func TestBuildFTSHitSet_Phrase_ExpandsToExactComponentWords(t *testing.T) {
	// PDF fulltext can't verify word adjacency (Zotero's fulltextItemWords is
	// a word index with no positional data). Quoted phrases therefore
	// degrade to AND-of-exact-words at the FTS layer — this is the honest
	// fallback; metadata and note-body surfaces still enforce adjacency.
	store := &mockFTSStore{hits: []int64{2}}
	groups := match.ParseClauses(`"gossip drives"`)
	_ = buildFTSHitSet(groups, store, "items")

	for _, c := range store.calls {
		if !c.exact {
			t.Errorf("phrase components must be searched exactly, got %+v", c)
		}
	}
	exactWords := lo.FlatMap(store.calls, func(c mockFTSCall, _ int) []string {
		if !c.exact {
			return nil
		}
		return c.words
	})
	if !slices.Contains(exactWords, "gossip") || !slices.Contains(exactWords, "drives") {
		t.Errorf("expected component words searched, got %v", exactWords)
	}
	// The raw phrase string must NOT be sent as a single word — Zotero's
	// word index has no entries containing spaces.
	if slices.Contains(exactWords, "gossip drives") {
		t.Error("must not send phrase as single word to SearchFulltext")
	}
}

// SnapshotPostPin usage — keep tests hermetic against future changes to
// default tab init.
var _ = tabstate.SnapshotPostPin
