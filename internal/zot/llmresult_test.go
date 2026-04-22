package zot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLLMCatalogResult_JSON(t *testing.T) {
	t.Parallel()
	r := LLMCatalogResult{
		Count: 2,
		Entries: []LLMCatalogEntry{
			{Key: "K1", Title: "Paper One", DOI: "10.1/a", Date: "2024-01-15", NoteKey: "N1", Tags: []string{"ml"}},
			{Key: "K2", Title: "Paper Two", NoteKey: "N2", IsHTML: true},
		},
	}
	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"key":"K1"`) {
		t.Errorf("missing key K1 in JSON: %s", s)
	}
	if !strings.Contains(s, `"is_html":true`) {
		t.Errorf("missing is_html flag in JSON: %s", s)
	}
}

func TestLLMCatalogResult_HumanEmpty(t *testing.T) {
	t.Parallel()
	r := LLMCatalogResult{Count: 0}
	h := r.Human()
	if !strings.Contains(h, "no docling notes") {
		t.Errorf("expected empty message; got: %s", h)
	}
}

func TestLLMCatalogResult_Human(t *testing.T) {
	t.Parallel()
	r := LLMCatalogResult{
		Count: 1,
		Entries: []LLMCatalogEntry{
			{Key: "K1", Title: "Paper One", DOI: "10.1/a", NoteKey: "N1", Tags: []string{"ml", "rl"}},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "K1") {
		t.Errorf("missing key: %s", h)
	}
	if !strings.Contains(h, "Paper One") {
		t.Errorf("missing title: %s", h)
	}
	if !strings.Contains(h, "1 paper(s)") {
		t.Errorf("missing count: %s", h)
	}
}

// TestLLMCatalogResult_HumanFull guards the --full renderer. When entries
// carry Citekey/Year/Authors/Abstract (populated by `--full` in
// llm_catalog.go) the human output must surface them; otherwise the flag
// is silently a no-op in TTY mode and users only see enrichment via --json.
func TestLLMCatalogResult_HumanFull(t *testing.T) {
	t.Parallel()
	r := LLMCatalogResult{
		Count: 1,
		Entries: []LLMCatalogEntry{{
			Key:          "K1",
			Citekey:      "smith2024-mypaper-K1",
			Title:        "Paper One",
			Year:         2024,
			DOI:          "10.1/a",
			Authors:      []string{"Alice Smith", "Bob Jones"},
			AuthorsTotal: 5,
			Abstract:     "We show that X predicts Y under Z conditions, extending prior work.",
			NoteKey:      "N1",
		}},
	}
	h := r.Human()
	for _, want := range []string{"Alice Smith", "et al.", "2024", "smith2024-mypaper-K1", "predicts Y"} {
		if !strings.Contains(h, want) {
			t.Errorf("full-mode human output missing %q:\n%s", want, h)
		}
	}
}

// TestLLMCatalogResult_HumanFull_Mixed covers the realistic case where a
// `--full` run has a mix of enriched entries (parent lookup succeeded) and
// plain ones (parent Read errored — see graceful fallback in llm_catalog.go).
// Enriched entries get the brief treatment; plain entries stay compact.
func TestLLMCatalogResult_HumanFull_Mixed(t *testing.T) {
	t.Parallel()
	r := LLMCatalogResult{
		Count: 2,
		Entries: []LLMCatalogEntry{
			{Key: "K1", Citekey: "ck-K1", Title: "Rich", Year: 2024, Authors: []string{"A"}, Abstract: "abs.", NoteKey: "N1"},
			{Key: "K2", Title: "Plain", DOI: "10.1/b", NoteKey: "N2"},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "ck-K1") || !strings.Contains(h, "2024") {
		t.Errorf("rich entry should show citekey+year:\n%s", h)
	}
	if !strings.Contains(h, "K2") || !strings.Contains(h, "Plain") || !strings.Contains(h, "10.1/b") {
		t.Errorf("plain entry should still show DOI:\n%s", h)
	}
}

func TestLLMReadResult_JSON(t *testing.T) {
	t.Parallel()
	r := LLMReadResult{
		Count: 1,
		Entries: []LLMReadEntry{
			{Key: "K1", Title: "Paper One", NoteKey: "N1", Body: "---\nfrontmatter\n---\n# Heading"},
		},
	}
	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"body":"---\nfrontmatter\n---\n# Heading"`) {
		t.Errorf("missing body in JSON: %s", s)
	}
}

func TestLLMReadResult_HumanEmpty(t *testing.T) {
	t.Parallel()
	r := LLMReadResult{Count: 0}
	h := r.Human()
	if !strings.Contains(h, "no notes found") {
		t.Errorf("expected empty message; got: %s", h)
	}
}

func TestLLMReadResult_Human(t *testing.T) {
	t.Parallel()
	r := LLMReadResult{
		Count: 1,
		Entries: []LLMReadEntry{
			{Key: "K1", Title: "Paper One", DOI: "10.1/a", NoteKey: "N1", Body: "# Content\nHello world"},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "K1") {
		t.Errorf("missing key: %s", h)
	}
	if !strings.Contains(h, "Paper One") {
		t.Errorf("missing title: %s", h)
	}
	if !strings.Contains(h, "# Content\nHello world") {
		t.Errorf("missing body: %s", h)
	}
}

func TestLLMQueryResult_JSON(t *testing.T) {
	t.Parallel()
	r := LLMQueryResult{
		MqQuery: ".h2",
		Matched: 1,
		Results: []LLMQueryMatch{
			{Key: "K1", Title: "Paper One", Output: "## Methods\n## Results"},
		},
	}
	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"mq_query":".h2"`) {
		t.Errorf("missing mq_query: %s", s)
	}
	if !strings.Contains(s, `"output":"## Methods\n## Results"`) {
		t.Errorf("missing output: %s", s)
	}
}

func TestLLMQueryResult_HumanEmpty(t *testing.T) {
	t.Parallel()
	r := LLMQueryResult{MqQuery: ".h", Matched: 0}
	h := r.Human()
	if !strings.Contains(h, "no matches") {
		t.Errorf("expected empty message; got: %s", h)
	}
}

func TestLLMQueryResult_Human(t *testing.T) {
	t.Parallel()
	r := LLMQueryResult{
		MqQuery: ".h2",
		Matched: 1,
		Results: []LLMQueryMatch{
			{Key: "K1", Title: "Paper One", Output: "## Methods"},
		},
	}
	h := r.Human()
	if !strings.Contains(h, "K1") {
		t.Errorf("missing key: %s", h)
	}
	if !strings.Contains(h, "Paper One") {
		t.Errorf("missing title: %s", h)
	}
	if !strings.Contains(h, "## Methods") {
		t.Errorf("missing output: %s", h)
	}
}

func TestLLMQueryResult_HumanSkipped(t *testing.T) {
	t.Parallel()
	r := LLMQueryResult{MqQuery: ".h", Matched: 1, Skipped: 2, Results: []LLMQueryMatch{
		{Key: "K1", Title: "P", Output: "# H"},
	}}
	h := r.Human()
	if !strings.Contains(h, "skipped 2") {
		t.Errorf("expected skipped count; got: %s", h)
	}
}
