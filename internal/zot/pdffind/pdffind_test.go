package pdffind

import (
	"context"
	"errors"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// --- test doubles -----------------------------------------------------------

type fakeLookup struct {
	works       map[string]*openalex.Work
	resolveErrs map[string]error
	searches    map[string][]openalex.Work
	searchErrs  map[string]error

	// counters for assertions
	resolves int
	searchs  int
}

func (l *fakeLookup) ResolveWork(_ context.Context, id string) (*openalex.Work, error) {
	l.resolves++
	if err, ok := l.resolveErrs[id]; ok {
		return nil, err
	}
	if w, ok := l.works[id]; ok {
		return w, nil
	}
	return nil, errors.New("not found: " + id)
}

func (l *fakeLookup) SearchWorks(_ context.Context, opts openalex.SearchOpts) (*openalex.Results[openalex.Work], error) {
	l.searchs++
	if err, ok := l.searchErrs[opts.Search]; ok {
		return nil, err
	}
	res := l.searches[opts.Search]
	return &openalex.Results[openalex.Work]{Results: res}, nil
}

func strPtr(s string) *string { return &s }

// --- tests ------------------------------------------------------------------

func TestScan_ResolvesByDOIWhenPresent(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "ABC", Title: "A paper", DOI: "10.1/x"},
	}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {
			ID:          "https://openalex.org/W42",
			DOI:         strPtr("https://doi.org/10.1/x"),
			Title:       strPtr("A paper"),
			IsOA:        true,
			HasFulltext: true,
			OpenAccess:  &openalex.OpenAccess{IsOA: true, OAStatus: "gold"},
			BestOALocation: &openalex.Location{
				PDFURL: strPtr("https://cdn.example.org/a.pdf"),
				IsOA:   true,
			},
			PrimaryLocation: &openalex.Location{
				LandingPageURL: strPtr("https://publisher.example.org/a"),
			},
		},
	}}

	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 1 || len(res.Findings) != 1 {
		t.Fatalf("want 1 finding, got %+v", res)
	}
	f := res.Findings[0]
	if f.LookupMethod != "doi" {
		t.Errorf("lookup method: got %q, want doi", f.LookupMethod)
	}
	if f.OpenAlexID != "W42" {
		t.Errorf("openalex id: got %q, want W42", f.OpenAlexID)
	}
	if f.OADOI != "10.1/x" {
		t.Errorf("oa doi: got %q, want 10.1/x (stripped)", f.OADOI)
	}
	if f.PDFURL != "https://cdn.example.org/a.pdf" {
		t.Errorf("pdf url: got %q", f.PDFURL)
	}
	if f.LandingPageURL != "https://publisher.example.org/a" {
		t.Errorf("landing page url: got %q", f.LandingPageURL)
	}
	if !f.IsOA || f.OAStatus != "gold" || !f.HasFulltext {
		t.Errorf("oa state wrong: %+v", f)
	}
	if oa.resolves != 1 || oa.searchs != 0 {
		t.Errorf("DOI path must not hit search: resolves=%d, searches=%d", oa.resolves, oa.searchs)
	}
}

func TestScan_FallsBackToTitleSearchWhenNoDOI(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "ABC", Title: "Attention is all you need"},
	}
	oa := &fakeLookup{searches: map[string][]openalex.Work{
		"Attention is all you need": {
			{
				ID:    "https://openalex.org/W999",
				DOI:   strPtr("https://doi.org/10.1/attn"),
				Title: strPtr("Attention Is All You Need"),
				BestOALocation: &openalex.Location{
					PDFURL: strPtr("https://arxiv.org/pdf/1706.03762"),
				},
			},
		},
	}}

	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Findings[0]
	if f.LookupMethod != "title" {
		t.Errorf("lookup method: got %q, want title", f.LookupMethod)
	}
	if f.OpenAlexID != "W999" {
		t.Errorf("openalex id: got %q", f.OpenAlexID)
	}
	if f.OADOI != "10.1/attn" {
		t.Errorf("OpenAlex DOI must be surfaced on no-local-DOI items, got %q", f.OADOI)
	}
	if f.PDFURL == "" {
		t.Errorf("want pdf url from title match")
	}
}

func TestScan_SkipsItemsWithNoDOIOrTitle(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "ABC"}}
	oa := &fakeLookup{}
	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := res.Findings[0]
	if f.LookupError == "" {
		t.Error("want lookup_error set when item has no DOI and no title")
	}
	if oa.resolves != 0 || oa.searchs != 0 {
		t.Errorf("no network calls expected, got resolves=%d searches=%d", oa.resolves, oa.searchs)
	}
}

func TestScan_RecordsDOIErrorsAndContinues(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "ABC", Title: "Broken", DOI: "10.1/broken"},
		{Key: "DEF", Title: "Good", DOI: "10.1/ok"},
	}
	oa := &fakeLookup{
		resolveErrs: map[string]error{"10.1/broken": errors.New("404")},
		works: map[string]*openalex.Work{
			"10.1/ok": {ID: "https://openalex.org/W1", Title: strPtr("Good")},
		},
	}
	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Findings[0].LookupError == "" {
		t.Error("want lookup_error on failed DOI")
	}
	if res.Findings[1].OpenAlexID != "W1" {
		t.Errorf("second item must still resolve: %+v", res.Findings[1])
	}
}

func TestScan_EmptyTitleSearchResultsRecordedAsError(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "ABC", Title: "Unfindable"}}
	oa := &fakeLookup{searches: map[string][]openalex.Work{}}
	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Findings[0].LookupError == "" {
		t.Error("want lookup_error on empty search results")
	}
}

func TestScan_FallsBackToPrimaryLocationPDFURL(t *testing.T) {
	t.Parallel()
	// best_oa_location has no PDF, but primary_location does — surface it.
	items := []local.Item{{Key: "ABC", DOI: "10.1/x"}}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {
			ID: "https://openalex.org/W1",
			PrimaryLocation: &openalex.Location{
				LandingPageURL: strPtr("https://publisher.example.org/a"),
				PDFURL:         strPtr("https://publisher.example.org/a.pdf"),
			},
		},
	}}
	res, err := Scan(context.Background(), items, oa, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Findings[0].PDFURL != "https://publisher.example.org/a.pdf" {
		t.Errorf("pdf url fallback: got %q", res.Findings[0].PDFURL)
	}
}

func TestScan_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := []local.Item{{Key: "A", DOI: "10.1/x"}, {Key: "B", DOI: "10.1/y"}}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {ID: "https://openalex.org/W1"},
		"10.1/y": {ID: "https://openalex.org/W2"},
	}}
	_, err := Scan(ctx, items, oa, ScanOptions{})
	if err == nil {
		t.Error("want ctx error")
	}
}

func TestScan_UsesCacheOnHit(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "ABC", DOI: "10.1/x"}}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {ID: "https://openalex.org/W42", Title: strPtr("From network")},
	}}
	cache := &Cache{Dir: t.TempDir()}

	// First scan populates the cache.
	if _, err := Scan(context.Background(), items, oa, ScanOptions{Cache: cache}); err != nil {
		t.Fatal(err)
	}
	if oa.resolves != 1 {
		t.Fatalf("first scan should hit network once, got %d", oa.resolves)
	}

	// Second scan must hit the cache — no new network calls.
	res, err := Scan(context.Background(), items, oa, ScanOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if oa.resolves != 1 {
		t.Errorf("second scan should NOT hit network, got %d resolves", oa.resolves)
	}
	if res.Findings[0].OpenAlexID != "W42" {
		t.Errorf("cached value not returned: %+v", res.Findings[0])
	}
}

func TestScan_RefreshBypassesCacheReadButStillWrites(t *testing.T) {
	t.Parallel()
	items := []local.Item{{Key: "ABC", DOI: "10.1/x"}}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {ID: "https://openalex.org/W42"},
	}}
	cache := &Cache{Dir: t.TempDir()}
	// Pre-populate with stale data.
	cache.Put("doi:10.1/x", Finding{ItemKey: "ABC", OpenAlexID: "STALE"})

	res, err := Scan(context.Background(), items, oa, ScanOptions{Cache: cache, Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Findings[0].OpenAlexID != "W42" {
		t.Errorf("refresh must return fresh data, got %+v", res.Findings[0])
	}
	if oa.resolves != 1 {
		t.Errorf("refresh must hit network, got %d resolves", oa.resolves)
	}
	// Fresh value must overwrite the cache.
	got, ok := cache.Get("doi:10.1/x")
	if !ok || got.OpenAlexID != "W42" {
		t.Errorf("refresh must update cache, got %+v ok=%v", got, ok)
	}
}

func TestScan_CallsOnItemCallback(t *testing.T) {
	t.Parallel()
	items := []local.Item{
		{Key: "A", DOI: "10.1/x"},
		{Key: "B", DOI: "10.1/y"},
	}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {ID: "https://openalex.org/W1"},
		"10.1/y": {ID: "https://openalex.org/W2"},
	}}
	cache := &Cache{Dir: t.TempDir()}
	cache.Put("doi:10.1/x", Finding{ItemKey: "A", OpenAlexID: "W1_cached"})

	type call struct {
		i, total int
		hit      bool
		key      string
	}
	var calls []call
	opts := ScanOptions{
		Cache: cache,
		OnItem: func(i, total int, f Finding, hit bool) {
			calls = append(calls, call{i, total, hit, f.ItemKey})
		},
	}
	if _, err := Scan(context.Background(), items, oa, opts); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 callback invocations, got %d", len(calls))
	}
	if !calls[0].hit {
		t.Errorf("item 0 should be cache hit: %+v", calls[0])
	}
	if calls[1].hit {
		t.Errorf("item 1 should be cache miss: %+v", calls[1])
	}
	if calls[0].total != 2 || calls[1].i != 1 {
		t.Errorf("index/total wrong: %+v", calls)
	}
}
