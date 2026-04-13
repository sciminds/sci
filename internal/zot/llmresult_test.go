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
