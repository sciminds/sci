package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

// Graph `cites` responses from OpenAlex regularly carry multi-hundred
// author papers (collaborations, consortium authors). The full list is
// useless for lit-review skimming and burns LLM-agent context. The
// default JSON shape caps authors at compactAuthorLimit; --verbose
// flips back to the full list.
func TestCmdResultJSON_compactTrimsAuthors(t *testing.T) {
	t.Parallel()
	big := make([]string, 50)
	for i := range big {
		big[i] = "Author " + string(rune('A'+i%26))
	}
	res := &Result{
		Item:      Source{Key: "SRC123", Title: "Source paper"},
		Direction: "cites",
		OutsideLibrary: []Neighbor{
			{OpenAlex: "W1", Title: "Huge collab", Authors: big},
			{OpenAlex: "W2", Title: "Small paper", Authors: big[:2]}, // under limit
		},
	}

	b, err := json.Marshal(CmdResult{Result: res}.JSON())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		OutsideLibrary []struct {
			Title        string   `json:"title"`
			Authors      []string `json:"authors"`
			AuthorsTotal int      `json:"authors_total"`
		} `json:"outside_library"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.OutsideLibrary) != 2 {
		t.Fatalf("want 2 neighbors, got %d", len(got.OutsideLibrary))
	}
	if n := got.OutsideLibrary[0]; len(n.Authors) != compactAuthorLimit || n.AuthorsTotal != 50 {
		t.Errorf("neighbor 0: want %d authors + total=50, got %d authors + total=%d", compactAuthorLimit, len(n.Authors), n.AuthorsTotal)
	}
	// Under-limit author list should be passed through untouched (no total marker).
	if n := got.OutsideLibrary[1]; len(n.Authors) != 2 || n.AuthorsTotal != 0 {
		t.Errorf("neighbor 1: want 2 authors + total=0, got %d authors + total=%d", len(n.Authors), n.AuthorsTotal)
	}
}

func TestCmdResultJSON_verbosePreservesFullAuthors(t *testing.T) {
	t.Parallel()
	big := make([]string, 50)
	for i := range big {
		big[i] = "A"
	}
	res := &Result{
		OutsideLibrary: []Neighbor{{OpenAlex: "W1", Authors: big}},
	}
	b, err := json.Marshal(CmdResult{Result: res, Verbose: true}.JSON())
	if err != nil {
		t.Fatal(err)
	}
	// Verbose returns the raw Result — no authors_total field exists on Neighbor.
	if strings.Contains(string(b), "authors_total") {
		t.Errorf("verbose JSON should not carry authors_total: %s", b)
	}
	var got struct {
		OutsideLibrary []struct {
			Authors []string `json:"authors"`
		} `json:"outside_library"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.OutsideLibrary[0].Authors) != 50 {
		t.Errorf("verbose: want 50 authors, got %d", len(got.OutsideLibrary[0].Authors))
	}
}

func TestCmdResultJSON_nilResult(t *testing.T) {
	t.Parallel()
	if got := (CmdResult{}).JSON(); got != nil {
		t.Errorf("nil Result should JSON to nil, got %#v", got)
	}
}
