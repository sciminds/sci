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

	res, err := Scan(context.Background(), items, oa)
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

	res, err := Scan(context.Background(), items, oa)
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
	res, err := Scan(context.Background(), items, oa)
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
	res, err := Scan(context.Background(), items, oa)
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
	res, err := Scan(context.Background(), items, oa)
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
	res, err := Scan(context.Background(), items, oa)
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
	_, err := Scan(ctx, items, oa)
	if err == nil {
		t.Error("want ctx error")
	}
}
