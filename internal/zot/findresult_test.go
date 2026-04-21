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
