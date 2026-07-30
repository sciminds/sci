package hygiene

import "testing"

func TestClusterByContent_GroupsIdenticalBytes(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{
		{Key: "AAAA1111", Title: "Remembering: A Study in Experimental and Social Psychology"},
		{Key: "BBBB2222", Title: "Remembering: An Experimental and Social Study"},
		{Key: "CCCC3333", Title: "Something else entirely"},
	}
	// The two Bartletts are the same scan under different titles — the
	// case the title clusterer cannot see.
	keys := map[string]string{
		"AAAA1111": "104857-abc123",
		"BBBB2222": "104857-abc123",
		"CCCC3333": "220011-def456",
	}

	got := ClusterByContent(cands, keys)
	if len(got) != 1 {
		t.Fatalf("clusters = %d, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.MatchType != "content" {
		t.Errorf("match_type = %q, want %q", c.MatchType, "content")
	}
	if c.Score != 1.0 {
		t.Errorf("score = %v, want 1.0 — byte identity is not a guess", c.Score)
	}
	if len(c.Members) != 2 {
		t.Fatalf("members = %d, want 2", len(c.Members))
	}
	for _, m := range c.Members {
		if m.Key != "AAAA1111" && m.Key != "BBBB2222" {
			t.Errorf("unexpected member %q", m.Key)
		}
	}
}

// An empty content key means the hash failed or the PDF is missing. It
// is not evidence of sameness, and grouping on it would cluster every
// unreadable PDF in the library into one giant false positive.
func TestClusterByContent_NeverGroupsOnEmptyKey(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{
		{Key: "AAAA1111"}, {Key: "BBBB2222"}, {Key: "CCCC3333"},
	}
	keys := map[string]string{"AAAA1111": "", "BBBB2222": ""}

	if got := ClusterByContent(cands, keys); len(got) != 0 {
		t.Errorf("clusters = %+v, want none — empty keys must never group", got)
	}
}

func TestClusterByContent_SingletonsAreNotClusters(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{{Key: "AAAA1111"}, {Key: "BBBB2222"}}
	keys := map[string]string{"AAAA1111": "1-a", "BBBB2222": "2-b"}

	if got := ClusterByContent(cands, keys); len(got) != 0 {
		t.Errorf("clusters = %+v, want none", got)
	}
}

func TestClusterByContent_IsDeterministic(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{
		{Key: "K1"}, {Key: "K2"}, {Key: "K3"}, {Key: "K4"},
	}
	keys := map[string]string{"K1": "z-2", "K2": "a-1", "K3": "z-2", "K4": "a-1"}

	first := ClusterByContent(cands, keys)
	for range 5 {
		got := ClusterByContent(cands, keys)
		if len(got) != len(first) {
			t.Fatalf("cluster count varies across runs: %d vs %d", len(got), len(first))
		}
		for i := range got {
			if got[i].Members[0].Key != first[i].Members[0].Key {
				t.Fatalf("cluster order varies across runs at %d", i)
			}
		}
	}
}

// A candidate with no entry in the map (no PDF at all) is simply not a
// content-duplicate candidate.
func TestClusterByContent_MissingKeysIgnored(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{{Key: "K1"}, {Key: "K2"}, {Key: "K3"}}
	keys := map[string]string{"K1": "1-a", "K2": "1-a"}

	got := ClusterByContent(cands, keys)
	if len(got) != 1 || len(got[0].Members) != 2 {
		t.Fatalf("want one 2-member cluster, got %+v", got)
	}
}

func TestRunDuplicates_ContentStrategy(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{
		// Same bytes, different titles and no DOIs: invisible to both
		// of the other strategies.
		{Key: "AAAA1111", Title: "Remembering: A Study in Experimental and Social Psychology"},
		{Key: "BBBB2222", Title: "Remembering: An Experimental and Social Study"},
	}
	opts := DuplicatesOptions{
		Strategy:    StrategyContent,
		ContentKeys: map[string]string{"AAAA1111": "104857-abc", "BBBB2222": "104857-abc"},
	}

	got := RunDuplicates(cands, opts)
	if len(got) != 1 || got[0].MatchType != "content" {
		t.Fatalf("want one content cluster, got %+v", got)
	}

	// The same pair under the default strategies finds nothing — which
	// is exactly why this strategy exists.
	if other := RunDuplicates(cands, DuplicatesOptions{Strategy: StrategyBoth}); len(other) != 0 {
		t.Errorf("doi/title strategies found %+v, expected nothing", other)
	}
}

// Content is byte identity — the strongest signal there is — so it must
// not be diluted by the weaker passes when it is the chosen strategy.
func TestRunDuplicates_ContentStrategyRunsAlone(t *testing.T) {
	t.Parallel()
	cands := []DuplicateCandidate{
		{Key: "AAAA1111", Title: "Same title", DOI: "10.1/x"},
		{Key: "BBBB2222", Title: "Same title", DOI: "10.1/x"},
	}
	opts := DuplicatesOptions{Strategy: StrategyContent, ContentKeys: map[string]string{
		"AAAA1111": "1-a", "BBBB2222": "2-b", // different bytes
	}}

	if got := RunDuplicates(cands, opts); len(got) != 0 {
		t.Errorf("content strategy emitted %+v — doi/title passes must not run", got)
	}
}
