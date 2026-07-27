package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sciminds/cli/internal/zot/bib"
	"github.com/sciminds/cli/internal/zot/doiorg"
	"github.com/sciminds/cli/internal/zot/openalex"
)

func writeFiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectBibTargets_File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "paper.qmd")
	got, err := collectBibTargets(filepath.Join(dir, "paper.qmd"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "paper.qmd" {
		t.Errorf("got %v", got)
	}
}

func TestCollectBibTargets_DirNonRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "b.md", "a.qmd", "notes.txt", "sub/deep.md")
	got, err := collectBibTargets(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.qmd"), filepath.Join(dir, "b.md")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectBibTargets_Recursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "a.md", "sub/deep.md", ".obsidian/config.md", "sub/skip.txt")
	got, err := collectBibTargets(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "sub", "deep.md")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v (hidden dirs must be skipped)", got, want)
	}
}

// fakeWorks stands in for *openalex.Client in the lookup adapter.
type fakeWorks struct {
	works map[string]*openalex.Work
	errs  map[string]error
	asked []string
}

func (f *fakeWorks) ResolveWork(_ context.Context, id string) (*openalex.Work, error) {
	f.asked = append(f.asked, id)
	if err, ok := f.errs[id]; ok {
		return nil, err
	}
	if w, ok := f.works[id]; ok {
		return w, nil
	}
	return nil, &openalex.StatusError{Path: "/works/" + id, Code: 404, Body: "Not Found"}
}

func TestOpenAlexLookup_MapsWorkToMatch(t *testing.T) {
	t.Parallel()
	title := "The proactive brain"
	doi := "https://doi.org/10.1016/j.tics.2007.05.005"
	year := 2007
	f := &fakeWorks{works: map[string]*openalex.Work{
		"10.1016/j.tics.2007.05.005": {
			ID: "https://openalex.org/W2119803923", DOI: &doi, Title: &title,
			PublicationYear: &year, IsRetracted: true,
			PrimaryLocation: &openalex.Location{Source: &openalex.SourceRef{DisplayName: "Trends in Cognitive Sciences"}},
		},
	}}
	got, err := (openAlexLookup{f}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindDOI, Value: "10.1016/j.tics.2007.05.005"})
	if err != nil {
		t.Fatal(err)
	}
	// The bare short id, not the openalex.org URL — that's what
	// `item add --openalex` accepts.
	if got.OpenAlexID != "W2119803923" {
		t.Errorf("openalex id = %q", got.OpenAlexID)
	}
	// The bare DOI, not the doi.org URL.
	if got.DOI != "10.1016/j.tics.2007.05.005" {
		t.Errorf("doi = %q", got.DOI)
	}
	if got.Title != title || got.Year != 2007 {
		t.Errorf("title/year = %q/%d", got.Title, got.Year)
	}
	if got.Venue != "Trends in Cognitive Sciences" {
		t.Errorf("venue = %q", got.Venue)
	}
	if !got.Retracted {
		t.Error("retraction flag dropped")
	}
}

func TestOpenAlexLookup_404IsErrNotFound(t *testing.T) {
	t.Parallel()
	_, err := (openAlexLookup{&fakeWorks{}}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindDOI, Value: "10.1234/invented"})
	if !errors.Is(err, bib.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestOpenAlexLookup_RateLimitIsNotNotFound — a 429 must not be reported as
// a fabricated citation.
func TestOpenAlexLookup_RateLimitIsNotNotFound(t *testing.T) {
	t.Parallel()
	f := &fakeWorks{errs: map[string]error{
		"10.1/real": &openalex.StatusError{Path: "/works/x", Code: 429, Body: "slow down"},
	}}
	_, err := (openAlexLookup{f}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindDOI, Value: "10.1/real"})
	if err == nil || errors.Is(err, bib.ErrNotFound) {
		t.Errorf("err = %v, want a non-ErrNotFound error", err)
	}
}

// TestOpenAlexLookup_ArxivIsPrefixed — a bare "1706.03762" is ambiguous to
// NormalizeID; the arxiv: prefix makes the lookup deterministic.
func TestOpenAlexLookup_ArxivIsPrefixed(t *testing.T) {
	t.Parallel()
	f := &fakeWorks{}
	_, _ = (openAlexLookup{f}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindArxiv, Value: "1706.03762"})
	if len(f.asked) != 1 || f.asked[0] != "arxiv:1706.03762" {
		t.Errorf("asked = %v", f.asked)
	}
}

// fakeRegistry stands in for *doiorg.Client.
type fakeRegistry struct {
	recs  map[string]*doiorg.Record
	asked []string
}

func (f *fakeRegistry) Resolve(_ context.Context, doi string) (*doiorg.Record, error) {
	f.asked = append(f.asked, doi)
	if r, ok := f.recs[doi]; ok {
		return r, nil
	}
	return nil, doiorg.ErrNotFound
}

func TestRegistryLookup_MapsRecordToMatch(t *testing.T) {
	t.Parallel()
	f := &fakeRegistry{recs: map[string]*doiorg.Record{
		"10.1093/acprof:oso/9780195367638.001.0001": {
			DOI:   "10.1093/acprof:oso/9780195367638.001.0001",
			Title: "The Origin of Concepts", Year: 2009, Venue: "Oxford University Press",
		},
	}}
	got, err := (registryLookup{f}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindDOI, Value: "10.1093/acprof:oso/9780195367638.001.0001"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Origin of Concepts" || got.Year != 2009 {
		t.Errorf("match = %+v", got)
	}
	// The registry knows nothing about retractions — it must not claim to.
	if got.Retracted {
		t.Error("registry invented a retraction flag")
	}
}

// TestRegistryLookup_ArxivGoesThroughItsDataCiteDOI — this is what stopped
// arXiv:1801.00173 being called fabricated.
func TestRegistryLookup_ArxivGoesThroughItsDataCiteDOI(t *testing.T) {
	t.Parallel()
	f := &fakeRegistry{recs: map[string]*doiorg.Record{
		"10.48550/arXiv.1801.00173": {Title: "Theory of Deep Learning III", Year: 2018},
	}}
	got, err := (registryLookup{f}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindArxiv, Value: "1801.00173"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Theory of Deep Learning III" {
		t.Errorf("match = %+v", got)
	}
	if len(f.asked) != 1 || f.asked[0] != "10.48550/arXiv.1801.00173" {
		t.Errorf("asked = %v", f.asked)
	}
}

func TestRegistryLookup_UnregisteredIsErrNotFound(t *testing.T) {
	t.Parallel()
	_, err := (registryLookup{&fakeRegistry{}}).ResolveRef(context.Background(),
		bib.Ref{Kind: bib.KindDOI, Value: "10.1234/invented"})
	if !errors.Is(err, bib.ErrNotFound) {
		t.Errorf("err = %v, want bib.ErrNotFound", err)
	}
}

// TestVerifyChain_CoverageGapIsNotFabrication is the regression line for the
// bug the first real-manuscript run exposed: OpenAlex 404s on Carey's
// monograph, the registry has it, and the verdict must be "add it", not
// "you made this up".
func TestVerifyChain_CoverageGapIsNotFabrication(t *testing.T) {
	t.Parallel()
	index := openAlexLookup{&fakeWorks{}} // 404s on everything
	registry := registryLookup{&fakeRegistry{recs: map[string]*doiorg.Record{
		"10.1093/acprof:oso/9780195367638.001.0001": {Title: "The Origin of Concepts", Year: 2009},
	}}}
	got := bib.Verify(context.Background(),
		[]bib.Unresolved{
			{Ref: bib.Ref{Kind: bib.KindDOI, Value: "10.1093/acprof:oso/9780195367638.001.0001"}, Reason: "no match"},
			{Ref: bib.Ref{Kind: bib.KindDOI, Value: "10.1234/invented"}, Reason: "no match"},
		},
		bib.ChainLookup{index, registry})

	if got[0].Status != bib.StatusExternal {
		t.Errorf("real monograph: status = %q, want %q", got[0].Status, bib.StatusExternal)
	}
	if got[1].Status != bib.StatusNotFound {
		t.Errorf("invented DOI: status = %q, want %q", got[1].Status, bib.StatusNotFound)
	}
}
