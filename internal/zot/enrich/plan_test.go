package enrich

import (
	"context"
	"errors"
	"testing"

	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// --- test doubles -----------------------------------------------------------

type fakeReader struct {
	local.Reader
	items map[string]*local.Item
}

func (r *fakeReader) Read(key string) (*local.Item, error) {
	if it, ok := r.items[key]; ok {
		return it, nil
	}
	return nil, errors.New("not found")
}

type fakeLookup struct {
	works map[string]*openalex.Work
	errs  map[string]error
}

func (l *fakeLookup) ResolveWork(_ context.Context, id string) (*openalex.Work, error) {
	if err, ok := l.errs[id]; ok {
		return nil, err
	}
	if w, ok := l.works[id]; ok {
		return w, nil
	}
	return nil, errors.New("not found: " + id)
}

// --- tests ------------------------------------------------------------------

func TestPlanFromMissing_fillsOnlyMissingFields(t *testing.T) {
	t.Parallel()
	// Item ABC has DOI + title already, but is missing abstract and creators.
	// OpenAlex has all four; the plan must fill only the two that are missing.
	db := &fakeReader{items: map[string]*local.Item{
		"ABC": {Key: "ABC", Version: 42, Type: "journalArticle", Title: "Existing Title", DOI: "10.1/x"},
	}}
	oa := &fakeLookup{works: map[string]*openalex.Work{
		"10.1/x": {
			ID:    "https://openalex.org/W1",
			Title: strPtr("OpenAlex Title"),
			Authorships: []openalex.Authorship{
				{Author: openalex.AuthorRef{DisplayName: "Alice Smith"}},
			},
			AbstractInvertedIndex: map[string][]int{
				"abstract": {0}, "from": {1}, "openalex": {2},
			},
		},
	}}

	findings := []hygiene.Finding{
		{Check: "missing", Kind: string(hygiene.FieldAbstract), ItemKey: "ABC"},
		{Check: "missing", Kind: string(hygiene.FieldCreators), ItemKey: "ABC"},
	}

	targets, skipped, err := PlanFromMissing(context.Background(), db, oa, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("unexpected skipped: %+v", skipped)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %d", len(targets))
	}
	tg := targets[0]
	if tg.ItemKey != "ABC" || tg.Version != 42 || tg.ItemType != "journalArticle" {
		t.Errorf("target metadata wrong: %+v", tg)
	}
	if _, ok := tg.Fills["title"]; ok {
		t.Errorf("title is present locally — must not be filled, got %+v", tg.Fills)
	}
	if _, ok := tg.Fills["abstract"]; !ok {
		t.Errorf("abstract is missing — must be filled, got %+v", tg.Fills)
	}
	if _, ok := tg.Fills["creators"]; !ok {
		t.Errorf("creators missing — must be filled, got %+v", tg.Fills)
	}
	if tg.Data.Title != nil {
		t.Errorf("patch body must leave Title nil (not missing): %v", tg.Data.Title)
	}
	if tg.Data.AbstractNote == nil {
		t.Errorf("patch body must set AbstractNote")
	}
	if tg.Data.Creators == nil || len(*tg.Data.Creators) != 1 {
		t.Errorf("patch body must set Creators")
	}
}

func TestPlanFromMissing_skipsItemsWithoutDOI(t *testing.T) {
	t.Parallel()
	db := &fakeReader{items: map[string]*local.Item{
		"ABC": {Key: "ABC", Version: 1, Type: "journalArticle", Title: "Some Paper"},
	}}
	oa := &fakeLookup{}
	findings := []hygiene.Finding{{Check: "missing", Kind: string(hygiene.FieldAbstract), ItemKey: "ABC"}}

	targets, skipped, err := PlanFromMissing(context.Background(), db, oa, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Errorf("unexpected targets: %+v", targets)
	}
	if len(skipped) != 1 || skipped[0].ItemKey != "ABC" {
		t.Fatalf("want 1 skip, got %+v", skipped)
	}
	if skipped[0].Reason == "" {
		t.Error("skip reason must be populated")
	}
}

func TestPlanFromMissing_dedupesFindingsByItemKey(t *testing.T) {
	t.Parallel()
	// Same item, three missing-field findings → one lookup, one target.
	db := &fakeReader{items: map[string]*local.Item{
		"ABC": {Key: "ABC", Version: 1, Type: "journalArticle", DOI: "10.1/x"},
	}}
	var lookups int
	oa := &lookupCountingStub{
		count: &lookups,
		inner: &fakeLookup{works: map[string]*openalex.Work{
			"10.1/x": {ID: "https://openalex.org/W1", Title: strPtr("T"), PublicationDate: strPtr("2024-01-01")},
		}},
	}
	findings := []hygiene.Finding{
		{Kind: string(hygiene.FieldTitle), ItemKey: "ABC"},
		{Kind: string(hygiene.FieldDate), ItemKey: "ABC"},
		{Kind: string(hygiene.FieldAbstract), ItemKey: "ABC"},
	}

	targets, _, err := PlanFromMissing(context.Background(), db, oa, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Errorf("want 1 target, got %d", len(targets))
	}
	if lookups != 1 {
		t.Errorf("want 1 lookup, got %d (must dedupe per item)", lookups)
	}
}

func TestPlanFromMissing_recordsLookupErrorsAsSkips(t *testing.T) {
	t.Parallel()
	db := &fakeReader{items: map[string]*local.Item{
		"ABC": {Key: "ABC", Version: 1, Type: "journalArticle", DOI: "10.1/x"},
	}}
	oa := &fakeLookup{errs: map[string]error{"10.1/x": errors.New("404")}}
	findings := []hygiene.Finding{{Kind: string(hygiene.FieldAbstract), ItemKey: "ABC"}}

	targets, skipped, err := PlanFromMissing(context.Background(), db, oa, findings)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Errorf("no targets expected on lookup error, got %+v", targets)
	}
	if len(skipped) != 1 || skipped[0].Reason == "" {
		t.Fatalf("want 1 skip with reason, got %+v", skipped)
	}
}

type lookupCountingStub struct {
	count *int
	inner Lookup
}

func (l *lookupCountingStub) ResolveWork(ctx context.Context, id string) (*openalex.Work, error) {
	*l.count++
	return l.inner.ResolveWork(ctx, id)
}
