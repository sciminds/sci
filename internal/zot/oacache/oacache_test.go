package oacache_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot/oacache"
	"github.com/sciminds/sci/pkg/openalex"
)

// fakeOA stands in for *openalex.Client. It records every request so the
// tests can assert on batching, which is the difference between 84 calls
// and 4,200.
type fakeOA struct {
	calls    []openalex.SearchOpts
	byDOI    map[string]openalex.Work
	byTitle  map[string][]openalex.Work
	count    int // Meta.Count to report; 0 means len(results)
	resolved []string
	byID     map[string]openalex.Work
	err      error
}

func (f *fakeOA) SearchWorks(_ context.Context, opts openalex.SearchOpts) (*openalex.Results[openalex.Work], error) {
	f.calls = append(f.calls, opts)
	if f.err != nil {
		return nil, f.err
	}
	var out []openalex.Work
	if d, ok := opts.Filter["doi"]; ok {
		for _, one := range strings.Split(d, "|") {
			if w, ok := f.byDOI[one]; ok {
				out = append(out, w)
			}
		}
	}
	if ids, ok := opts.Filter["openalex_id"]; ok {
		for _, one := range strings.Split(ids, "|") {
			if w, ok := f.byID[one]; ok {
				out = append(out, w)
			}
		}
	}
	if t, ok := opts.Filter["title.search"]; ok {
		// The server unquotes before searching; so does the fake, or every
		// lookup would miss for a reason the real API does not have.
		out = append(out, f.byTitle[strings.Trim(t, `"`)]...)
	}
	meta := openalex.ResultsMeta{Count: len(out)}
	if f.count > 0 {
		meta.Count = f.count
	}
	if opts.PerPage > 0 && len(out) > opts.PerPage {
		out = out[:opts.PerPage]
	}
	return &openalex.Results[openalex.Work]{Results: out, Meta: meta}, nil
}

func work(id, doi, title string) openalex.Work {
	return openalex.Work{ID: "https://openalex.org/" + id, DOI: &doi, DisplayName: &title}
}

func TestDOIsAreFetchedInBatchesNotOneByOne(t *testing.T) {
	f := &fakeOA{byDOI: map[string]openalex.Work{}}
	var dois []string
	for i := range 120 {
		d := "10.1/" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		dois = append(dois, d)
		f.byDOI[d] = work("W"+d, "https://doi.org/"+d, "T"+d)
	}
	res, err := oacache.Fetch(context.Background(), f, oacache.Want{DOIs: dois}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// 120 DOIs at 50 per filter is three requests. One request per DOI is
	// the difference between a sync that finishes and one that burns the
	// daily API budget.
	if len(f.calls) != 3 {
		t.Errorf("made %d requests for 120 DOIs, want 3", len(f.calls))
	}
	if res.Stats.Works != 120 {
		t.Errorf("kept %d works, want 120", res.Stats.Works)
	}
}

func TestEveryWorkIsAskedForTheFullFieldMask(t *testing.T) {
	f := &fakeOA{byDOI: map[string]openalex.Work{}}
	if _, err := oacache.Fetch(context.Background(), f,
		oacache.Want{DOIs: []string{"10.1/x"}}, oacache.Options{}); err != nil {
		t.Fatal(err)
	}
	sel := strings.Join(f.calls[0].Select, ",")
	// The caches this replaces were fetched with a narrower select, so the
	// consumer could not tell an absent field from a null one and every
	// work carried a NULL year. Asking for the columns the schema has is
	// the whole point of re-fetching.
	for _, field := range []string{"publication_year", "type", "display_name", "referenced_works"} {
		if !strings.Contains(sel, field) {
			t.Errorf("select does not ask for %s: %s", field, sel)
		}
	}
}

func TestADOIOpenAlexDoesNotHaveIsNamedNotDropped(t *testing.T) {
	f := &fakeOA{byDOI: map[string]openalex.Work{
		"10.1/have": work("W1", "https://doi.org/10.1/have", "Held"),
	}}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{DOIs: []string{"10.1/have", "10.1/missing"}}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// OpenAlex returns what it has and says nothing about what it lacks, so
	// a caller that does not diff request against response cannot tell a
	// miss from a hit. Recording the list, not just a count, is what makes
	// the claim checkable -- and it is a claim about OPENALEX's coverage,
	// never about whether the paper exists.
	if res.Stats.DOIsNotFound != 1 || len(res.NotFound) != 1 {
		t.Fatalf("not-found = %d %v, want exactly 10.1/missing", res.Stats.DOIsNotFound, res.NotFound)
	}
	if res.NotFound[0] != "10.1/missing" {
		t.Errorf("not-found names %q", res.NotFound[0])
	}
	if res.Stats.DOIsFound != 1 {
		t.Errorf("found = %d, want 1", res.Stats.DOIsFound)
	}
}

func TestDOIComparisonSurvivesTheResolverPrefix(t *testing.T) {
	// Zotero stores a bare DOI; OpenAlex answers with the URL form. A
	// literal comparison reports every single DOI as not found while the
	// works land in the cache perfectly -- a coverage report that is
	// entirely wrong and entirely plausible.
	f := &fakeOA{byDOI: map[string]openalex.Work{
		"10.1/x": work("W1", "https://doi.org/10.1/X", "Cased"),
	}}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{DOIs: []string{"10.1/x"}}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.DOIsNotFound != 0 {
		t.Errorf("not-found = %v, want none", res.NotFound)
	}
}

func TestTitleLookupsReturnCandidatesAndJudgeNothing(t *testing.T) {
	f := &fakeOA{byTitle: map[string][]openalex.Work{
		"Theory of Mind": {
			work("W1", "https://doi.org/10.1/a", "Theory of Mind"),
			work("W2", "https://doi.org/10.1/b", "Theory of mind: a review"),
		},
	}}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{Titles: []string{"Theory of Mind"}}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Both candidates are kept. Deciding which one IS the library item is
	// resolution, and resolution lives in zot where title_norm is defined
	// exactly once; filtering here would be the second definition.
	if res.Stats.Works != 2 {
		t.Errorf("kept %d candidates, want both", res.Stats.Works)
	}
	if res.Stats.TitlesWithHits != 1 {
		t.Errorf("titles_with_hits = %d, want 1", res.Stats.TitlesWithHits)
	}
}

func TestACappedCandidateListSaysSo(t *testing.T) {
	f := &fakeOA{
		byTitle: map[string][]openalex.Work{"Attention": {
			work("W1", "https://doi.org/10.1/a", "Attention"),
			work("W2", "https://doi.org/10.1/b", "Attention"),
			work("W3", "https://doi.org/10.1/c", "Attention"),
		}},
		count: 900, // OpenAlex says there are far more
	}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{Titles: []string{"Attention"}}, oacache.Options{TitleCandidates: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Works != 2 {
		t.Errorf("kept %d, want the cap of 2", res.Stats.Works)
	}
	// A cap is a truncation. Applying one quietly is how a partial answer
	// gets read as a complete one.
	if res.Stats.TitlesTruncated != 1 {
		t.Errorf("titles_truncated = %d, want 1", res.Stats.TitlesTruncated)
	}
}

func TestTheSameWorkReachedTwiceIsStoredOnce(t *testing.T) {
	w := work("W1", "https://doi.org/10.1/a", "Shared")
	f := &fakeOA{
		byDOI:   map[string]openalex.Work{"10.1/a": w},
		byTitle: map[string][]openalex.Work{"Shared": {w}},
	}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{DOIs: []string{"10.1/a"}, Titles: []string{"Shared"}}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A duplicated work would give it two rows in the pool, and zot refuses
	// an ambiguous match -- so a dedup bug here silently REDUCES resolution.
	if res.Stats.Works != 1 {
		t.Errorf("kept %d copies of one work", res.Stats.Works)
	}
}

func TestSidecarIsWrittenLastAndVouchesForTheBody(t *testing.T) {
	dir := t.TempDir()
	res := oacache.Result{
		Works:    []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
		Stats:    oacache.Stats{Works: 1, DOIsRequested: 2, DOIsFound: 1, DOIsNotFound: 1},
		NotFound: []string{"10.1/gone"},
	}
	body, err := oacache.Write(dir, "all", res)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(body + ".meta.json")
	if err != nil {
		t.Fatalf("no sidecar beside the body: %v", err)
	}
	var meta struct {
		RecordsTotal int      `json:"records_total"`
		SHA256       string   `json:"sha256"`
		NotFound     []string `json:"not_found"`
		Select       []string `json:"select"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.RecordsTotal != 1 || len(meta.SHA256) != 64 {
		t.Errorf("sidecar does not describe the body: %+v", meta)
	}
	// The not-found list rides into the sidecar so the consumer inherits
	// the gap instead of rediscovering it.
	if len(meta.NotFound) != 1 {
		t.Error("the sidecar dropped the not-found list")
	}
	if len(meta.Select) == 0 {
		t.Error("the sidecar does not record which fields were requested")
	}
	// The body is NDJSON: one work per line, no wrapper.
	lines, err := os.ReadFile(filepath.Join(dir, oacache.CacheFile))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(string(lines)), "\n"); n != 0 {
		t.Errorf("one work produced %d newlines inside the body", n)
	}
}

func (f *fakeOA) ResolveWork(_ context.Context, id string) (*openalex.Work, error) {
	f.resolved = append(f.resolved, id)
	if w, ok := f.byDOI[id]; ok {
		return &w, nil
	}
	return nil, errNotFound
}

var errNotFound = errors.New("not found")

func TestAFilterHostileDOIDoesNotPoisonItsBatch(t *testing.T) {
	// Kluwer minted 10.1023/a:NNN and Wiley minted the SICI form; both are
	// perfectly resolvable DOIs that happen to contain a colon. Inside a
	// `doi:a|b|c` filter that colon makes OpenAlex parse the rest of the
	// batch as a second filter and reject the whole request — so ONE such
	// value silently loses the other 49 lookups.
	f := &fakeOA{byDOI: map[string]openalex.Work{
		"10.1/plain":              work("W1", "https://doi.org/10.1/plain", "Plain"),
		"10.1023/a:1007465528199": work("W2", "https://doi.org/10.1023/a:1007465528199", "Kluwer"),
	}}
	res, err := oacache.Fetch(context.Background(), f, oacache.Want{
		DOIs: []string{"10.1/plain", "10.1023/a:1007465528199"},
	}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The colon-bearing one went down the path route instead.
	if len(f.resolved) != 1 || f.resolved[0] != "10.1023/a:1007465528199" {
		t.Errorf("resolved individually = %v, want the colon-bearing DOI", f.resolved)
	}
	for _, c := range f.calls {
		if strings.Contains(c.Filter["doi"], ":") {
			t.Errorf("a filter metacharacter reached a batch: %q", c.Filter["doi"])
		}
	}
	if res.Stats.Works != 2 || res.Stats.DOIsNotFound != 0 {
		t.Errorf("stats = %+v, want both works found", res.Stats)
	}
	if res.Stats.DOIsUnbatchable != 1 {
		t.Errorf("dois_unbatchable = %d, want 1", res.Stats.DOIsUnbatchable)
	}
}

func TestAnUnresolvableSoloDOIIsReportedNotSwallowed(t *testing.T) {
	f := &fakeOA{byDOI: map[string]openalex.Work{}}
	res, err := oacache.Fetch(context.Background(), f,
		oacache.Want{DOIs: []string{"10.1/junk;pageGroup:string:Publication"}}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A failed individual lookup must land in the same not-found list as a
	// batch miss, or the two routes report coverage differently.
	if len(res.NotFound) != 1 {
		t.Errorf("not-found = %v, want the unresolvable DOI", res.NotFound)
	}
}

func TestATitleWithACommaIsEscapedNotSentRaw(t *testing.T) {
	f := &fakeOA{byTitle: map[string][]openalex.Work{}}
	title := `Untangling the Relatedness among Correlations, Part III`
	if _, err := oacache.Fetch(context.Background(), f,
		oacache.Want{Titles: []string{title}}, oacache.Options{}); err != nil {
		t.Fatal(err)
	}
	got := f.calls[0].Filter["title.search"]
	// Percent-encoding is not enough: OpenAlex decodes the param and only
	// then splits on commas, so the escape has to be the quoting their docs
	// specify. Academic titles are full of commas, so getting this wrong
	// fails on the very first lookup and takes the whole run with it.
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Errorf("title filter = %q, want it quoted", got)
	}
	if !strings.Contains(got, "Part III") {
		t.Errorf("quoting mangled the title: %q", got)
	}
}

func TestAQuoteInATitleCannotCloseTheWrapperEarly(t *testing.T) {
	got := oacache.TitleFilter(`On "Theory" of Mind`)
	if strings.Count(got, `"`) != 2 {
		t.Errorf("filter = %q, want exactly the two wrapping quotes", got)
	}
}

// TestWorkSelectCoversEveryFieldTheWorkStructDecodes pins the invariant
// WorkSelect's own comment states: ask for everything the schema has a
// column for.
//
// It drifted in both directions at once, and neither direction announces
// itself. abstract_inverted_index was decoded but never requested, so a
// full sync wrote 6,618 works with zero abstracts — indistinguishable, to
// every consumer downstream, from a corpus whose publishers supply none.
// updated_date was requested but decoded nowhere, so every response paid
// for a field that was thrown away on arrival.
//
// Reflecting over the struct rather than restating the list is the whole
// point: a hand-maintained mask beside a hand-maintained struct is two
// lists that must agree, which is how they came to disagree.
func TestWorkSelectCoversEveryFieldTheWorkStructDecodes(t *testing.T) {
	want := map[string]bool{}
	rt := reflect.TypeFor[openalex.Work]()
	for i := range rt.NumField() {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			want[tag] = true
		}
	}

	got := map[string]bool{}
	for _, f := range oacache.WorkSelect {
		if got[f] {
			t.Errorf("WorkSelect lists %q twice", f)
		}
		got[f] = true
	}

	for f := range want {
		if !got[f] {
			t.Errorf("openalex.Work decodes %q but WorkSelect never asks for it — "+
				"the field arrives null and reads as absent-from-OpenAlex", f)
		}
	}
	for f := range got {
		if !want[f] {
			t.Errorf("WorkSelect asks for %q but openalex.Work has no field for it — "+
				"every response carries it and nothing reads it", f)
		}
	}
}

// TestANotFoundDOIFallsBackToItsTitle covers a trap the DOI backfill
// walked straight into.
//
// wantFrom sends an item down the DOI path if it has one and the title
// path otherwise -- never both -- on the reasonable-sounding theory that a
// DOI is the stronger identifier. It is, right up until OpenAlex has no
// record of it. Then having a DOI is strictly WORSE than having none: the
// lookup returns nothing AND the title lookup that would have matched was
// never made.
//
// This is not hypothetical. Writing 709 inferred DOIs into Zotero gave 20
// items a DOI OpenAlex does not index -- Oxford Scholarship chapters,
// Kluwer, Wiley SICI, arXiv -- and 10 of them lost the title match they
// used to have and fell to a local mint. They were resolvable before the
// backfill and unresolvable after it.
func TestANotFoundDOIFallsBackToItsTitle(t *testing.T) {
	t.Parallel()
	f := &fakeOA{
		byDOI: map[string]openalex.Work{},
		byTitle: map[string][]openalex.Work{
			"The vectors of mind": {{ID: "https://openalex.org/W7"}},
		},
	}
	res, err := oacache.Fetch(context.Background(), f, oacache.Want{
		DOIs:           []string{"10.1037/h0075959"},
		FallbackTitles: map[string]string{"10.1037/h0075959": "The vectors of mind"},
	}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Works) != 1 {
		t.Fatalf("kept %d works, want the title fallback's hit", len(res.Works))
	}
	// The DOI is still reported not-found: the fallback recovers a WORK,
	// it does not make OpenAlex's coverage of that DOI any better, and a
	// sidecar that quietly dropped it would overstate the index.
	if len(res.NotFound) != 1 {
		t.Errorf("not_found = %v, want the DOI still listed", res.NotFound)
	}
	if res.Stats.FallbackTitlesQueried != 1 || res.Stats.FallbackTitlesWithHits != 1 {
		t.Errorf("stats = %+v", res.Stats)
	}
}

func TestAFoundDOINeverCostsAFallbackRequest(t *testing.T) {
	t.Parallel()
	doi := "10.1/found"
	f := &fakeOA{
		byDOI:   map[string]openalex.Work{doi: {ID: "https://openalex.org/W1", DOI: &doi}},
		byTitle: map[string][]openalex.Work{"A Paper": {{ID: "https://openalex.org/W2"}}},
	}
	res, err := oacache.Fetch(context.Background(), f, oacache.Want{
		DOIs:           []string{doi},
		FallbackTitles: map[string]string{doi: "A Paper"},
	}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The fallback is for MISSES only. Firing it on every DOI would double
	// the metered cost of a sync to buy nothing.
	if res.Stats.FallbackTitlesQueried != 0 {
		t.Errorf("a found DOI triggered %d fallback lookups", res.Stats.FallbackTitlesQueried)
	}
	if len(res.Works) != 1 {
		t.Errorf("kept %d works", len(res.Works))
	}
}

func TestFallbackLookupsAreCountedSeparately(t *testing.T) {
	t.Parallel()
	f := &fakeOA{
		byDOI:   map[string]openalex.Work{},
		byTitle: map[string][]openalex.Work{"Recovered": {{ID: "https://openalex.org/W9"}}},
	}
	res, err := oacache.Fetch(context.Background(), f, oacache.Want{
		DOIs:           []string{"10.1/missing"},
		Titles:         []string{"A DOI-less item"},
		FallbackTitles: map[string]string{"10.1/missing": "Recovered"},
	}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// TitlesQueried is the count of items that had NO DOI. Folding
	// fallbacks into it would make the sync's own report unable to say
	// how much of the run was spent recovering from bad DOIs.
	if res.Stats.TitlesQueried != 1 {
		t.Errorf("TitlesQueried = %d, want only the DOI-less item", res.Stats.TitlesQueried)
	}
	if res.Stats.FallbackTitlesQueried != 1 {
		t.Errorf("FallbackTitlesQueried = %d", res.Stats.FallbackTitlesQueried)
	}
}

// The reference title pool — the thing that lets `zot cites` NAME the work
// an edge points at — had no producer. It was a zen-ingest artifact copied
// into staging by hand, frozen at one date, and covering ~72% of what the
// library actually cites: 53,573 cited works had no title at all, 29% of
// the citation graph.
//
// The set is not a new query. Every work's referenced_works names it, and
// this verb has just fetched those works, so the pool derives from the
// same pass that produced them and the two can never disagree about which
// works are cited.
func TestCitedWorksAreCollectedFromTheWorksJustFetched(t *testing.T) {
	parent := work("W1", "10.1/a", "Parent")
	parent.ReferencedWorks = []string{
		"https://openalex.org/W10", "https://openalex.org/W11",
		"https://openalex.org/W10", // a repeat costs no request
		"https://openalex.org/W1",  // already a full record; not re-fetched
	}
	f := &fakeOA{byID: map[string]openalex.Work{
		"W10": work("W10", "10.1/j", "Cited ten"),
		"W11": work("W11", "10.1/k", "Cited eleven"),
	}}

	got, err := oacache.FetchCited(context.Background(), f, []openalex.Work{parent}, nil, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Works) != 2 {
		t.Fatalf("cited works = %d, want 2 (deduped, self excluded)", len(got.Works))
	}
	// A narrow mask here is deliberate — whole records for 186k cited works
	// is a different product. Deliberate is not the same as unrecorded:
	// this one has to reach the sidecar or its omissions are invisible.
	sel := strings.Join(f.calls[0].Select, ",")
	for _, field := range []string{"id", "display_name", "publication_year", "doi", "type"} {
		if !strings.Contains(sel, field) {
			t.Errorf("cited select does not ask for %s: %s", field, sel)
		}
	}
	if strings.Contains(sel, "abstract_inverted_index") || strings.Contains(sel, "authorships") {
		t.Errorf("cited select is not narrow: %s", sel)
	}
}

func TestTheCitedPoolPublishesTheMaskThatBuiltIt(t *testing.T) {
	dir := t.TempDir()
	res := oacache.CitedResult{Works: []openalex.Work{work("W10", "10.1/j", "Cited ten")}}
	if _, err := oacache.WriteCited(dir, "all", res); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, oacache.CitedFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m oacache.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Select, oacache.CitedSelect) {
		t.Errorf("sidecar select = %v, want %v", m.Select, oacache.CitedSelect)
	}
	if m.RecordsTotal != 1 || m.SHA256 == "" {
		t.Errorf("sidecar does not describe the body: %+v", m)
	}
}

// ---------------------------------------------------------------------------
// Targeted delta syncs
// ---------------------------------------------------------------------------

// readLines returns the body's NDJSON lines, so a merge can be checked for
// what it did NOT touch. Byte-identity is the assertion that matters: a
// merge that re-encodes every record would look correct in every field and
// still be a full rewrite of a file the consumer digests.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // a test's own temp dir
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// writeRawBase plants a body the way OpenAlex might actually have left one:
// records carrying a field this build's Work type does not model.
//
// That is what makes the byte-identity assertion able to fail. A base
// written through Write encodes the same struct the merge would re-encode,
// so a merge that decoded and re-encoded every record would produce
// identical bytes and the test would pass while doing the wrong thing. With
// an unmodelled field present, a re-encoding merge silently DROPS it — the
// field-mask failure, arriving through the back door.
func writeRawBase(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestADeltaReplacesItsTargetAndLeavesEveryOtherLineByteIdentical(t *testing.T) {
	dir := t.TempDir()
	writeRawBase(t, dir, oacache.CacheFile,
		`{"id":"https://openalex.org/W1","doi":"https://doi.org/10.1/a","display_name":"One"}`,
		`{"id":"https://openalex.org/W2","doi":"https://doi.org/10.1/b","display_name":"Two","apc_paid":{"value":3000}}`)
	before := readLines(t, filepath.Join(dir, oacache.CacheFile))

	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	fresh := oacache.Result{Works: []openalex.Work{work("W1", "https://doi.org/10.1/a", "One, revised")}}
	_, merge, err := oacache.WriteDelta(dir, "all", fresh, base, &oacache.Delta{Keys: []string{"AAAAAAAA"}})
	if err != nil {
		t.Fatal(err)
	}
	if merge.Replaced != 1 || merge.Added != 0 || merge.Total != 2 {
		t.Fatalf("merge = %+v, want one replacement and nothing else", merge)
	}
	after := readLines(t, filepath.Join(dir, oacache.CacheFile))
	if len(after) != 2 {
		t.Fatalf("body has %d lines, want 2", len(after))
	}
	// A merge NARROWS nothing: the record it did not fetch is the same
	// bytes it was, so the field mask that produced it survives untouched.
	if after[1] != before[1] {
		t.Errorf("an untouched record was rewritten:\n before %s\n after  %s", before[1], after[1])
	}
	if !strings.Contains(after[1], "apc_paid") {
		t.Error("a field this build does not model was dropped by a merge that never fetched that record")
	}
	if !strings.Contains(after[0], "One, revised") {
		t.Errorf("the targeted record was not replaced: %s", after[0])
	}
	if after[0] == before[0] {
		t.Error("the targeted record is unchanged")
	}
}

func TestADeltaAppendsWorksTheBaseNeverHeldAndDuplicatesNone(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.Write(dir, "all", oacache.Result{Works: []openalex.Work{
		work("W1", "https://doi.org/10.1/a", "One"),
	}}); err != nil {
		t.Fatal(err)
	}
	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	fresh := oacache.Result{Works: []openalex.Work{
		work("W1", "https://doi.org/10.1/a", "One, revised"),
		work("W9", "https://doi.org/10.1/i", "Nine"),
	}}
	_, merge, err := oacache.WriteDelta(dir, "all", fresh, base, &oacache.Delta{})
	if err != nil {
		t.Fatal(err)
	}
	if merge.Replaced != 1 || merge.Added != 1 || merge.Total != 2 {
		t.Fatalf("merge = %+v, want one replaced and one added", merge)
	}
	// A duplicated id gives one work two rows, and zot refuses an ambiguous
	// match — so a merge that appended instead of replacing would silently
	// REDUCE resolution while growing the file.
	seen := map[string]int{}
	for _, line := range readLines(t, filepath.Join(dir, oacache.CacheFile)) {
		var rec struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		seen[rec.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times after a merge", id, n)
		}
	}
	if len(seen) != 2 {
		t.Errorf("merged body holds %d works, want 2", len(seen))
	}
}

func TestADeltaSidecarNamesTheBaseItMergedOver(t *testing.T) {
	dir := t.TempDir()
	full := oacache.Result{
		Works: []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
		Stats: oacache.Stats{Works: 1, DOIsRequested: 5158, Requests: 857},
	}
	if _, err := oacache.Write(dir, "all", full); err != nil {
		t.Fatal(err)
	}
	baseRaw, err := os.ReadFile(filepath.Join(dir, oacache.CacheFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var baseMeta oacache.Meta
	if err := json.Unmarshal(baseRaw, &baseMeta); err != nil {
		t.Fatal(err)
	}

	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	fresh := oacache.Result{
		Works: []openalex.Work{work("W9", "https://doi.org/10.1/i", "Nine")},
		Stats: oacache.Stats{Works: 1, DOIsRequested: 1, DOIsFound: 1, Requests: 1},
	}
	if _, _, err := oacache.WriteDelta(dir, "all", fresh, base, &oacache.Delta{
		Keys: []string{"ABCD1234"}, ItemsTargeted: 1, Requests: 2,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, oacache.CacheFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m oacache.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Delta == nil {
		t.Fatal("the sidecar does not say the body was merged, so a reader cannot tell a delta from a full sync")
	}
	if !slices.Equal(m.Delta.Keys, []string{"ABCD1234"}) {
		t.Errorf("delta.keys = %v, want the targeted key", m.Delta.Keys)
	}
	if m.Delta.RecordsAdded != 1 || m.Delta.RecordsReplaced != 0 {
		t.Errorf("delta = %+v, want one added record", m.Delta)
	}
	if m.Delta.Requests != 2 {
		t.Errorf("delta.requests = %d, want what the run actually spent", m.Delta.Requests)
	}
	// The base's identity is what makes the merge auditable: a sidecar that
	// only reported the result cannot say WHICH file the untouched records
	// came from, so a merge over the wrong base looks exactly like a merge
	// over the right one.
	if m.Delta.Base.SHA256 != baseMeta.SHA256 || m.Delta.Base.Records != 1 {
		t.Errorf("delta.base = %+v, want the base's own digest %s", m.Delta.Base, baseMeta.SHA256)
	}
	if m.Delta.Base.File != oacache.CacheFile {
		t.Errorf("delta.base.file = %q", m.Delta.Base.File)
	}
	if m.RecordsTotal != 2 {
		t.Errorf("records_total = %d, want the merged total", m.RecordsTotal)
	}
	if m.SHA256 == baseMeta.SHA256 {
		t.Error("the sidecar re-published the base's digest instead of the merged body's")
	}
	// The mask is the field-coverage claim. A delta merges records fetched
	// with the full mask into records fetched with the full mask, so the
	// claim is unchanged — publishing a narrower one would be the staleness
	// bug wearing a fingerprint's clothes.
	if !slices.Equal(m.Select, oacache.WorkSelect) {
		t.Errorf("select = %v, want the full mask", m.Select)
	}
	// stats is what THIS run did; a delta that overwrote it with small
	// numbers and nothing else would tell the pipeline's estimator that a
	// full sync costs one request.
	if m.Stats.Requests != 1 {
		t.Errorf("stats.requests = %d, want the delta's own spend", m.Stats.Requests)
	}
	if m.FullSyncStats == nil || m.FullSyncStats.Requests != 857 {
		t.Errorf("full_sync_stats = %+v, want the base's 857-request measurement carried forward", m.FullSyncStats)
	}
}

func TestADeltaOverASecondDeltaStillCarriesTheFullSyncMeasurement(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.Write(dir, "all", oacache.Result{
		Works: []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
		Stats: oacache.Stats{Requests: 857},
	}); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		base, err := oacache.LoadBase(dir, oacache.CacheFile)
		if err != nil {
			t.Fatal(err)
		}
		fresh := oacache.Result{
			Works: []openalex.Work{work("W"+string(rune('2'+i)), "https://doi.org/10.1/x", "X")},
			Stats: oacache.Stats{Requests: 1},
		}
		if _, _, err := oacache.WriteDelta(dir, "all", fresh, base, &oacache.Delta{}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, oacache.CacheFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m oacache.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// A chain of deltas must not erode the one number the leash prices a
	// full run from. Carrying it only one hop would make the second delta
	// the moment the pipeline decides a full sync is free.
	if m.FullSyncStats == nil || m.FullSyncStats.Requests != 857 {
		t.Errorf("full_sync_stats = %+v after two deltas", m.FullSyncStats)
	}
}

func TestThereIsNoDeltaWithoutABaseToMergeOver(t *testing.T) {
	// A delta that wrote its handful of records into an empty directory
	// would produce a file with the name, the shape and the sidecar of a
	// whole cache — and zot would load it as the whole cache, silently
	// dropping every work it does not mention.
	_, err := oacache.LoadBase(t.TempDir(), oacache.CacheFile)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist so the caller can refuse the run", err)
	}
}

func TestTheCitedPoolIsMergedNotReplaced(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.WriteCited(dir, "all", oacache.CitedResult{Works: []openalex.Work{
		work("W10", "10.1/j", "Cited ten"),
	}}); err != nil {
		t.Fatal(err)
	}
	base, err := oacache.LoadBase(dir, oacache.CitedFile)
	if err != nil {
		t.Fatal(err)
	}
	_, merge, err := oacache.WriteCitedDelta(dir, "all", oacache.CitedResult{
		Works: []openalex.Work{work("W11", "10.1/k", "Cited eleven")}, Asked: 1,
	}, base, &oacache.Delta{})
	if err != nil {
		t.Fatal(err)
	}
	if merge.Added != 1 || merge.Total != 2 {
		t.Fatalf("merge = %+v, want the pool grown by one", merge)
	}
	raw, err := os.ReadFile(filepath.Join(dir, oacache.CitedFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m oacache.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.RecordsTotal != 2 {
		t.Errorf("records_total = %d, want the merged pool", m.RecordsTotal)
	}
	// The leash prices the cited arm from this number. A delta that wrote
	// only its own two records would tell the runner the whole pool is two
	// works and that a full sync's cited arm costs one request.
	if !slices.Equal(m.Select, oacache.CitedSelect) {
		t.Errorf("select = %v, want the cited mask", m.Select)
	}
}

func TestCitedHydrationSkipsIdsTheCacheAlreadyHolds(t *testing.T) {
	parent := work("W1", "10.1/a", "Parent")
	parent.ReferencedWorks = []string{"https://openalex.org/W10", "https://openalex.org/W11"}
	f := &fakeOA{byID: map[string]openalex.Work{
		"W10": work("W10", "10.1/j", "Cited ten"),
		"W11": work("W11", "10.1/k", "Cited eleven"),
	}}
	// The pool already names W11. Re-fetching it buys the identical narrow
	// record at full price — and a targeted sync exists precisely because
	// requests are the scarce thing.
	got, err := oacache.FetchCited(context.Background(), f, []openalex.Work{parent},
		map[string]bool{"W11": true}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Asked != 1 || len(got.Works) != 1 {
		t.Fatalf("asked %d and got %d, want only the id the cache lacks", got.Asked, len(got.Works))
	}
	if len(f.calls) != 1 || strings.Contains(f.calls[0].Filter["openalex_id"], "W11") {
		t.Errorf("filter = %q, want W11 left out", f.calls[0].Filter["openalex_id"])
	}
	if got.Requests != 1 {
		t.Errorf("requests = %d, want the one batch it made", got.Requests)
	}
}

func TestAnEmptyCitedPlanCostsNothing(t *testing.T) {
	parent := work("W1", "10.1/a", "Parent")
	parent.ReferencedWorks = []string{"https://openalex.org/W10"}
	f := &fakeOA{byID: map[string]openalex.Work{}}
	got, err := oacache.FetchCited(context.Background(), f, []openalex.Work{parent},
		map[string]bool{"W10": true}, oacache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 || got.Requests != 0 {
		t.Errorf("made %d requests for a plan with nothing to fetch", len(f.calls))
	}
}

func TestEstimatePricesAPlanWithoutSpendingARequest(t *testing.T) {
	// N DOIs and M titles, priced from the same batching the fetch uses:
	// 118 batchable DOIs are three requests, the two filter-hostile ones go
	// one at a time, and each title is its own lookup.
	var dois []string
	for i := range 118 {
		dois = append(dois, "10.1/"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	dois = append(dois, "10.1023/a:1007465528199", "10.1002/(sici)1:5<3::aid-hbm1>3.0.co;2-5")
	plan := oacache.Estimate(oacache.Want{DOIs: dois, Titles: []string{"A", "B", "C"}})

	if plan.DOIRequests != 5 {
		t.Errorf("doi_requests = %d, want ceil(118/50)=3 batched plus 2 unbatchable", plan.DOIRequests)
	}
	if plan.DOIsUnbatchable != 2 {
		t.Errorf("dois_unbatchable = %d, want 2", plan.DOIsUnbatchable)
	}
	if plan.TitleRequests != 3 {
		t.Errorf("title_requests = %d, want one per title", plan.TitleRequests)
	}
	if plan.CitedRequests != 123 {
		t.Errorf("cited_requests = %d, want one per lookup", plan.CitedRequests)
	}
	if plan.Requests != 131 {
		t.Errorf("requests = %d, want 5+3+123", plan.Requests)
	}
	// The fallback arm is bounded but not priced: it fires once per DOI
	// OpenAlex turns out not to hold, measured at under 2% of them. Folding
	// the worst case into the headline number would triple every estimate
	// and defer runs that cost eight requests.
	if plan.FallbackMax != 120 || plan.RequestsMax != 251 {
		t.Errorf("fallback_max = %d, requests_max = %d", plan.FallbackMax, plan.RequestsMax)
	}
}

func TestASingleNewPaperIsPricedInSingleDigits(t *testing.T) {
	// The number the unattended pipeline's 50-request cap gets compared
	// against for the ordinary case: one paper arrived, it has a DOI.
	plan := oacache.Estimate(oacache.Want{DOIs: []string{"10.1/new"}})
	if plan.Requests != 2 || plan.RequestsMax != 3 {
		t.Errorf("plan = %+v, want 1 DOI lookup + 1 cited batch", plan)
	}
}

func TestABodyItsSidecarDoesNotVouchForIsNotAMergeableBase(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.Write(dir, "all", oacache.Result{
		Works: []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
	}); err != nil {
		t.Fatal(err)
	}
	// A body that grew after its sidecar was written is the shape of a run
	// that died between the two. The sidecar goes last precisely so that is
	// detectable; merging over it would launder half a write into a
	// complete-looking cache.
	f, err := os.OpenFile(filepath.Join(dir, oacache.CacheFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"https://openalex.org/W2"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := oacache.LoadBase(dir, oacache.CacheFile); err == nil {
		t.Fatal("loaded a base its own sidecar does not describe")
	}
}

func TestADeltaKeepsTheNotFoundListACorpusFactNotARunFact(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.Write(dir, "all", oacache.Result{
		Works:    []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
		NotFound: []string{"10.1/monograph", "10.1/late"},
	}); err != nil {
		t.Fatal(err)
	}
	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	// This run asked about 10.1/late (OpenAlex has it now) and 10.1/new
	// (it does not). It never asked about 10.1/monograph.
	fresh := oacache.Result{
		Works:    []openalex.Work{work("W2", "https://doi.org/10.1/late", "Late")},
		NotFound: []string{"10.1/new"},
	}
	if _, _, err := oacache.WriteDelta(dir, "all", fresh, base, &oacache.Delta{
		DOIsAsked: []string{"10.1/late", "10.1/new"},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, oacache.CacheFile+".meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m oacache.Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	// The list says "we asked OpenAlex and it has nothing" about the whole
	// corpus. A delta that published only its own two answers would shrink
	// it to those two, and the next --missing run would pay again for every
	// monograph OpenAlex has never indexed.
	if !slices.Equal(m.NotFound, []string{"10.1/monograph", "10.1/new"}) {
		t.Errorf("not_found = %v, want the untouched miss kept and the recovered one dropped", m.NotFound)
	}
}

func TestAWriteStampsTheDeltaItWasGiven(t *testing.T) {
	dir := t.TempDir()
	if _, err := oacache.Write(dir, "all", oacache.Result{
		Works: []openalex.Work{work("W1", "https://doi.org/10.1/a", "One")},
	}); err != nil {
		t.Fatal(err)
	}
	base, err := oacache.LoadBase(dir, oacache.CacheFile)
	if err != nil {
		t.Fatal(err)
	}
	d := oacache.Delta{Keys: []string{"ABCD1234"}}
	if _, _, err := oacache.WriteDelta(dir, "all", oacache.Result{
		Works: []openalex.Work{work("W9", "https://doi.org/10.1/i", "Nine")},
	}, base, &d); err != nil {
		t.Fatal(err)
	}
	// The sidecar and the caller's own report describe the same run. When
	// the write filled a COPY, the sidecar named its base and the CLI's
	// --json said `"base": {"file": "", "records": 0}` — two accounts of one
	// merge, both published.
	if d.Base.File != oacache.CacheFile || d.Base.Records != 1 || d.RecordsAdded != 1 {
		t.Errorf("delta = %+v, want the write's own accounting stamped back", d)
	}
}
