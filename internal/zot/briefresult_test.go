package zot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// ToBrief compacts a fully-hydrated item into the LLM-agent-friendly shape
// emitted by `search --full` and `llm catalog --full`. Two invariants
// pinned here because they're load-bearing for the token-efficiency
// goal: author lists get truncated past briefAuthorLimit, and Citekey
// is populated via citekey.Enrich so callers never see an empty key
// when the Fields map has one.
func TestToBrief_compactAuthors(t *testing.T) {
	t.Parallel()
	creators := make([]local.Creator, 10)
	for i := range creators {
		creators[i] = local.Creator{Type: "author", Last: "Author" + string(rune('A'+i))}
	}
	it := &local.Item{
		Key:      "ABC12345",
		Title:    "Big collab paper",
		Creators: creators,
	}
	b := ToBrief(it)
	if len(b.Authors) != briefAuthorLimit {
		t.Errorf("Authors len = %d, want %d", len(b.Authors), briefAuthorLimit)
	}
	if b.AuthorsTotal != 10 {
		t.Errorf("AuthorsTotal = %d, want 10", b.AuthorsTotal)
	}
}

func TestToBrief_shortAuthorListPassthrough(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key: "X",
		Creators: []local.Creator{
			{Type: "author", Last: "Smith"},
			{Type: "author", Last: "Jones"},
		},
	}
	b := ToBrief(it)
	if len(b.Authors) != 2 {
		t.Errorf("Authors len = %d, want 2", len(b.Authors))
	}
	if b.AuthorsTotal != 0 {
		// AuthorsTotal stays 0 (omitempty'd from JSON) when no truncation happened.
		t.Errorf("AuthorsTotal = %d, want 0 (no trim)", b.AuthorsTotal)
	}
}

func TestToBrief_populatesCitekeyFromFields(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:    "X",
		Fields: map[string]string{"citationKey": "smith2024-paper-X"},
	}
	b := ToBrief(it)
	if b.Citekey != "smith2024-paper-X" {
		t.Errorf("Citekey = %q, want smith2024-paper-X", b.Citekey)
	}
}

func TestToBrief_handlesInstitutionCreator(t *testing.T) {
	t.Parallel()
	it := &local.Item{
		Key:      "X",
		Creators: []local.Creator{{Type: "author", Name: "NASA"}},
	}
	b := ToBrief(it)
	if len(b.Authors) != 1 || b.Authors[0] != "NASA" {
		t.Errorf("institution creator mangled: %#v", b.Authors)
	}
}

// ItemBrief's JSON shape elides empty fields. Real libraries have many
// items with no DOI / abstract / tags — these mustn't pollute every hit.
func TestItemBrief_JSONOmitempty(t *testing.T) {
	t.Parallel()
	b := ItemBrief{Key: "X", Title: "Y"}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"citekey", "year", "doi", "publication", "authors", "abstract", "tags", "authors_total"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("expected %s elided, got: %s", field, raw)
		}
	}
}
