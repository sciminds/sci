package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// stubReader implements local.Reader with only the methods graph uses
// (ItemKeysByDOI). Other methods panic if accidentally called — keeps
// test surface narrow and surfaces unintended dependencies on more of
// the reader contract.
type stubReader struct {
	local.Reader
	dois map[string]string
}

func (s *stubReader) ItemKeysByDOI(dois []string) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range dois {
		if k, ok := s.dois[strings.ToLower(d)]; ok {
			out[strings.ToLower(d)] = k
		}
	}
	return out, nil
}

// newOpenAlex stands up an httptest server pretending to be the OpenAlex
// API. /works/{id} returns the supplied work; /works (no path segment)
// returns the supplied list. Either may be nil — the handler still
// responds with a JSON null in that case, exercising the package's
// nil-tolerance.
func newOpenAlex(t *testing.T, work *openalex.Work, list *openalex.Results[openalex.Work]) *openalex.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /works/{id} has a path segment after /works/, /works alone is the
		// search list. Strip query string before deciding.
		path := r.URL.Path
		if strings.HasPrefix(path, "/works/") {
			_ = json.NewEncoder(w).Encode(work)
			return
		}
		if path == "/works" {
			_ = json.NewEncoder(w).Encode(list)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	c := openalex.NewClient("", "")
	c.BaseURL = srv.URL
	return c
}

func ptr[T any](v T) *T { return &v }

func TestRefs_SplitsLibraryAndOutside(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{
		ID:              "https://openalex.org/W3105657479",
		Title:           ptr("MuZero"),
		ReferencedWorks: []string{"https://openalex.org/W1", "https://openalex.org/W2"},
	}
	hydrated := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{
			ID:              "https://openalex.org/W1",
			Title:           ptr("PlaNet"),
			DOI:             ptr("https://doi.org/10.48550/arxiv.1811.04551"),
			PublicationYear: ptr(2018),
			CitedByCount:    487,
		},
		{
			ID:              "https://openalex.org/W2",
			Title:           ptr("AlphaZero"),
			DOI:             ptr("https://doi.org/10.1126/science.aar6404"),
			PublicationYear: ptr(2017),
			CitedByCount:    4231,
		},
	}}
	oa := newOpenAlex(t, parent, hydrated)

	db := &stubReader{dois: map[string]string{
		"10.48550/arxiv.1811.04551": "537GTS3P", // PlaNet is in the library
	}}

	item := &local.Item{
		Key:   "C3VQHQ86",
		Title: "Mastering Atari, Go, chess and shogi",
		Extra: "OpenAlex: W3105657479",
	}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if res.Direction != "refs" {
		t.Errorf("direction = %q, want refs", res.Direction)
	}
	if res.Item.OpenAlex != "W3105657479" {
		t.Errorf("source openalex = %q", res.Item.OpenAlex)
	}
	if res.Stats.Total != 2 || res.Stats.InLibrary != 1 || res.Stats.OutsideLibrary != 1 {
		t.Errorf("stats = %+v, want 2/1/1", res.Stats)
	}
	if len(res.InLibrary) != 1 || res.InLibrary[0].Key != "537GTS3P" {
		t.Errorf("in_library = %+v, want PlaNet w/ Zotero key", res.InLibrary)
	}
	if len(res.OutsideLibrary) != 1 || res.OutsideLibrary[0].Title != "AlphaZero" {
		t.Errorf("outside_library = %+v, want AlphaZero", res.OutsideLibrary)
	}
	// Outside-library entry must carry an openalex id so agents can
	// follow up with `item add --openalex W…`.
	if res.OutsideLibrary[0].OpenAlex == "" {
		t.Error("outside neighbor missing openalex id")
	}
}

func TestRefs_MissingDOIGoesToOutside(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{
		ID:              "https://openalex.org/W9000001",
		ReferencedWorks: []string{"https://openalex.org/W9000002"},
	}
	hydrated := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{ID: "https://openalex.org/W9000002", Title: ptr("DOI-less preprint")},
	}}
	oa := newOpenAlex(t, parent, hydrated)
	db := &stubReader{dois: map[string]string{}}

	item := &local.Item{Key: "X", Extra: "OpenAlex: W9000001"}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.MissingMetadata != 1 {
		t.Errorf("missing_metadata = %d, want 1", res.Stats.MissingMetadata)
	}
	if len(res.OutsideLibrary) != 1 {
		t.Errorf("DOI-less neighbor should still surface in outside_library")
	}
}

func TestRefs_NoAnchorErrors(t *testing.T) {
	t.Parallel()
	oa := newOpenAlex(t, nil, nil)
	db := &stubReader{}

	item := &local.Item{Key: "X"} // no Extra, no DOI
	_, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{})
	if err != ErrNoOpenAlexAnchor {
		t.Errorf("err = %v, want ErrNoOpenAlexAnchor", err)
	}
}

// TestRefs_LimitTruncates_KeepsInLibraryFirst verifies that Limit caps
// total neighbors emitted, with in_library entries preserved first
// (they're the high-signal subset agents care about most). Stats keeps
// the pre-truncation totals so callers know truncation happened.
func TestRefs_LimitTruncates_KeepsInLibraryFirst(t *testing.T) {
	t.Parallel()
	// 5 referenced works: 2 in library, 3 outside.
	refs := []string{}
	results := []openalex.Work{}
	for i := 1; i <= 5; i++ {
		ref := fmt.Sprintf("https://openalex.org/W%d", 1000+i)
		refs = append(refs, ref)
		results = append(results, openalex.Work{
			ID:    ref,
			Title: ptr(fmt.Sprintf("Work %d", i)),
			DOI:   ptr(fmt.Sprintf("10.1000/w%d", i)),
		})
	}
	parent := &openalex.Work{ID: "https://openalex.org/W9000900", ReferencedWorks: refs}
	hydrated := &openalex.Results[openalex.Work]{Results: results}
	oa := newOpenAlex(t, parent, hydrated)

	db := &stubReader{dois: map[string]string{
		"10.1000/w1": "INLIB1",
		"10.1000/w2": "INLIB2",
	}}

	item := &local.Item{Key: "X", Extra: "OpenAlex: W9000900"}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}

	// Stats should reflect originals.
	if res.Stats.Total != 5 || res.Stats.InLibrary != 2 || res.Stats.OutsideLibrary != 3 {
		t.Errorf("stats = %+v, want 5/2/3 (pre-truncation totals)", res.Stats)
	}
	// Output: 2 in_library + 1 outside_library (limit=3, in_library always wins).
	if len(res.InLibrary) != 2 {
		t.Errorf("InLibrary len = %d, want 2 (kept first)", len(res.InLibrary))
	}
	if len(res.OutsideLibrary) != 1 {
		t.Errorf("OutsideLibrary len = %d, want 1 (filling remaining slot)", len(res.OutsideLibrary))
	}
}

func TestRefs_LimitZero_Unlimited(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{
		ID:              "https://openalex.org/W9000900",
		ReferencedWorks: []string{"https://openalex.org/W1"},
	}
	hydrated := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{ID: "https://openalex.org/W1", Title: ptr("Single")},
	}}
	oa := newOpenAlex(t, parent, hydrated)
	db := &stubReader{}

	item := &local.Item{Key: "X", Extra: "OpenAlex: W9000900"}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.OutsideLibrary) != 1 {
		t.Errorf("Limit 0 should NOT truncate; got OutsideLibrary len = %d", len(res.OutsideLibrary))
	}
}

func TestRefs_DOIFallbackResolves(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{ID: "https://openalex.org/W9000003", Title: ptr("via DOI")}
	oa := newOpenAlex(t, parent, &openalex.Results[openalex.Work]{})
	db := &stubReader{}
	item := &local.Item{Key: "X", DOI: "10.1000/anything"}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Item.OpenAlex != "W9000003" {
		t.Errorf("DOI fallback should populate OpenAlex id, got %+v", res.Item)
	}
}

func TestCites_FiltersAndSplits(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{ID: "https://openalex.org/W9000010"}
	citing := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{
			ID:           "https://openalex.org/W9000011",
			Title:        ptr("Follow-up A"),
			DOI:          ptr("10.1000/citerA"),
			CitedByCount: 100,
		},
		{
			ID:           "https://openalex.org/W9000012",
			Title:        ptr("Follow-up B"),
			CitedByCount: 50,
		},
	}}
	oa := newOpenAlex(t, parent, citing)
	db := &stubReader{dois: map[string]string{"10.1000/citera": "INLIB001"}}

	item := &local.Item{Key: "X", Extra: "OpenAlex: W9000010", Title: "Main"}
	res, err := Cites(context.Background(), LocalIndex(db), oa, item, CitesOpts{Limit: 10, YearMin: 2020})
	if err != nil {
		t.Fatal(err)
	}
	if res.Direction != "cites" {
		t.Errorf("direction = %q, want cites", res.Direction)
	}
	if len(res.InLibrary) != 1 || res.InLibrary[0].Key != "INLIB001" {
		t.Errorf("in_library = %+v", res.InLibrary)
	}
	if len(res.OutsideLibrary) != 1 || res.OutsideLibrary[0].OpenAlex != "W9000012" {
		t.Errorf("outside_library = %+v", res.OutsideLibrary)
	}
}

// TestRefs_ExcludesSelf — OpenAlex sometimes returns the source paper as
// one of its own ReferencedWorks (self-citation in the data). graph
// drops it so the agent doesn't see "this paper cites itself".
func TestRefs_ExcludesSelf(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{
		ID: "https://openalex.org/W4379279965",
		ReferencedWorks: []string{
			"https://openalex.org/W4379279965", // self
			"https://openalex.org/W2",
		},
	}
	hydrated := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{ID: "https://openalex.org/W4379279965", Title: ptr("Self-cite")},
		{ID: "https://openalex.org/W2", Title: ptr("Real reference"), DOI: ptr("10.1/real")},
	}}
	oa := newOpenAlex(t, parent, hydrated)
	db := &stubReader{}

	item := &local.Item{Key: "EV2PU9T8", Extra: "OpenAlex: W4379279965"}
	res, err := Refs(context.Background(), LocalIndex(db), oa, item, RefsOpts{})
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range slices.Concat(res.InLibrary, res.OutsideLibrary) {
		if n.OpenAlex == "W4379279965" {
			t.Errorf("self-loop leaked: %+v", n)
		}
	}
	if res.Stats.Total != 1 {
		t.Errorf("stats.Total = %d, want 1 (post-self-exclusion)", res.Stats.Total)
	}
}

// TestCites_ExcludesSelf — same self-loop edge case as Refs but on the
// cites direction. OpenAlex's cited_by index can include the source.
func TestCites_ExcludesSelf(t *testing.T) {
	t.Parallel()
	parent := &openalex.Work{ID: "https://openalex.org/W4379279965"}
	citing := &openalex.Results[openalex.Work]{Results: []openalex.Work{
		{ID: "https://openalex.org/W4379279965", Title: ptr("Self in cites")},
		{ID: "https://openalex.org/WCITER", Title: ptr("Real citer"), DOI: ptr("10.1/citer")},
	}}
	oa := newOpenAlex(t, parent, citing)
	db := &stubReader{}

	item := &local.Item{Key: "EV2PU9T8", Extra: "OpenAlex: W4379279965"}
	res, err := Cites(context.Background(), LocalIndex(db), oa, item, CitesOpts{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range slices.Concat(res.InLibrary, res.OutsideLibrary) {
		if n.OpenAlex == "W4379279965" {
			t.Errorf("self-loop leaked: %+v", n)
		}
	}
	if res.Stats.Total != 1 {
		t.Errorf("stats.Total = %d, want 1 (post-self-exclusion)", res.Stats.Total)
	}
}

func TestStripDOIScheme(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://doi.org/10.1/x":   "10.1/x",
		"http://dx.doi.org/10.1/x": "10.1/x",
		"10.1/x":                   "10.1/x",
		"":                         "",
	}
	for in, want := range cases {
		if got := stripDOIScheme(in); got != want {
			t.Errorf("stripDOIScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
