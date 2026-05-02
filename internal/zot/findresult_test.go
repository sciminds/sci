package zot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/openalex"
)

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

func TestFindWorksResult_Empty(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{Query: "transformers"}
	out := r.Human()
	if !strings.Contains(out, "no results") || !strings.Contains(out, "transformers") {
		t.Errorf("empty Human() = %q", out)
	}
}

func TestFindWorksResult_Human_lists(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{
		Query: "attention",
		Total: 137,
		Count: 1,
		Works: []openalex.Work{{
			ID:              "https://openalex.org/W2963403868",
			Title:           sp("Attention Is All You Need"),
			PublicationYear: ip(2017),
			DOI:             sp("https://doi.org/10.5555/3295222.3295349"),
			Authorships: []openalex.Authorship{
				{Author: openalex.AuthorRef{DisplayName: "Ashish Vaswani"}},
				{Author: openalex.AuthorRef{DisplayName: "Noam Shazeer"}},
			},
			PrimaryLocation: &openalex.Location{
				Source: &openalex.SourceRef{DisplayName: "Neural Information Processing Systems"},
			},
		}},
	}
	out := r.Human()
	if !strings.Contains(out, "W2963403868") {
		t.Errorf("missing short id: %q", out)
	}
	if !strings.Contains(out, "Attention Is All You Need") {
		t.Errorf("missing title: %q", out)
	}
	if !strings.Contains(out, "2017") {
		t.Errorf("missing year: %q", out)
	}
	if !strings.Contains(out, "Vaswani") {
		t.Errorf("missing author: %q", out)
	}
	if !strings.Contains(out, "10.5555/3295222.3295349") {
		// DOI should be displayed without the https://doi.org/ wrapper.
		t.Errorf("missing/unstripped DOI: %q", out)
	}
	if !strings.Contains(out, "137") {
		t.Errorf("missing total count: %q", out)
	}
}

func TestFindWorksResult_JSON_shape(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{Query: "x", Total: 1, Count: 0, Works: []openalex.Work{}}
	b, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{`"query"`, `"total"`, `"count"`, `"works"`} {
		if !strings.Contains(s, key) {
			t.Errorf("JSON missing %s: %s", key, s)
		}
	}
}

func TestFindWorksResult_JSON_compactByDefault(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{
		Query: "x",
		Count: 1,
		Works: []openalex.Work{{
			ID:              "https://openalex.org/W2963403868",
			DOI:             sp("https://doi.org/10.1000/abc"),
			Title:           sp("A Paper"),
			PublicationYear: ip(2017),
			Type:            sp("article"),
			CitedByCount:    42,
			IsOA:            true,
			Authorships: []openalex.Authorship{
				{Author: openalex.AuthorRef{DisplayName: "Ashish Vaswani"},
					Institutions:          []openalex.Institution{{ID: "https://openalex.org/I20089843", DisplayName: "Princeton University"}},
					RawAffiliationStrings: []string{"long string that should NOT appear"}},
			},
			PrimaryLocation: &openalex.Location{Source: &openalex.SourceRef{DisplayName: "NeurIPS"}},
			OpenAccess:      &openalex.OpenAccess{OAStatus: "gold"},
		}},
	}
	b, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// Compact fields should appear — flat names, no nesting.
	for _, want := range []string{`"openalex_id":"W2963403868"`, `"doi":"10.1000/abc"`, `"title":"A Paper"`, `"year":2017`, `"venue":"NeurIPS"`, `"oa_status":"gold"`, `"cited_by_count":42`, `"authors":["Ashish Vaswani"]`} {
		if !strings.Contains(s, want) {
			t.Errorf("compact JSON missing %s\nfull: %s", want, s)
		}
	}
	// Raw OpenAlex nested noise must be stripped.
	for _, unwanted := range []string{"raw_affiliation_strings", "institutions", "abstract_inverted_index", "orcid", "ror"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("compact JSON leaked %q: %s", unwanted, s)
		}
	}
}

func TestFindWorksResult_JSON_verbosePassesThroughRaw(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{
		Query:   "x",
		Count:   1,
		Verbose: true,
		Works: []openalex.Work{{
			ID:           "https://openalex.org/W1",
			CitedByCount: 1,
			Authorships: []openalex.Authorship{
				{RawAffiliationStrings: []string{"raw affiliation"}},
			},
		}},
	}
	b, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "raw_affiliation_strings") {
		t.Errorf("verbose JSON should pass raw fields through: %s", s)
	}
}

// TestFindWorksResult_LibraryHits — when the CLI passes a populated
// LibraryHits map, both the compact JSON shape and the human renderer
// surface in_library/library_key for matching DOIs. Other works stay
// unmarked so an agent can pick "what to add" by filtering on
// `in_library == false`.
func TestFindWorksResult_LibraryHits(t *testing.T) {
	t.Parallel()
	r := FindWorksResult{
		Query: "x",
		Count: 2,
		Works: []openalex.Work{
			{ID: "https://openalex.org/W1", DOI: sp("https://doi.org/10.1000/inlib"), Title: sp("Already in library")},
			{ID: "https://openalex.org/W2", DOI: sp("https://doi.org/10.1000/elsewhere"), Title: sp("Not yet")},
		},
		LibraryHits: map[string]string{
			"10.1000/inlib": "ZKEY1234",
		},
	}
	// Compact JSON: only the matching work carries in_library/library_key.
	b, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"in_library":true`) || !strings.Contains(s, `"library_key":"ZKEY1234"`) {
		t.Errorf("hit work missing in_library/library_key: %s", s)
	}
	// The non-hit work must NOT carry these fields (omitempty).
	if strings.Count(s, `"in_library"`) != 1 {
		t.Errorf("expected exactly one in_library marker, got: %s", s)
	}
	// Human output: green ✓ + key on the hit row.
	human := r.Human()
	if !strings.Contains(human, "ZKEY1234") {
		t.Errorf("human output missing in-library key marker: %s", human)
	}
}

func TestFindAuthorsResult_Human(t *testing.T) {
	t.Parallel()
	r := FindAuthorsResult{
		Query: "vaswani",
		Total: 3,
		Count: 1,
		Authors: []openalex.Author{{
			ID:           "https://openalex.org/A5061940714",
			DisplayName:  "Ashish Vaswani",
			ORCID:        sp("https://orcid.org/0000-0002-1234-5678"),
			WorksCount:   42,
			CitedByCount: 120000,
		}},
	}
	out := r.Human()
	if !strings.Contains(out, "A5061940714") || !strings.Contains(out, "Vaswani") {
		t.Errorf("Human() = %q", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("missing works count: %q", out)
	}
}
