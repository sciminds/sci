package hygiene

import (
	"sort"
	"testing"
)

// makeCands is a test helper for building a slice of DuplicateCandidate
// values with positional arguments in the order (key, title, doi).
// Date and PDFCount default to zero values — fuzz them in specific tests
// that care.
func makeCands(rows ...[3]string) []DuplicateCandidate {
	out := make([]DuplicateCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, DuplicateCandidate{
			Key:   r[0],
			Title: r[1],
			DOI:   r[2],
		})
	}
	return out
}

func clusterKeys(c Cluster) []string {
	keys := make([]string, len(c.Members))
	for i, m := range c.Members {
		keys[i] = m.Key
	}
	sort.Strings(keys)
	return keys
}

func TestClusterByDOI_ExactMatch(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "Paper one", "10.1000/foo"},
		[3]string{"A2", "Paper one (imported)", "10.1000/foo"},
		[3]string{"A3", "Different paper", "10.1000/bar"},
	)
	clusters := ClusterByDOI(cands)
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(clusters))
	}
	c := clusters[0]
	if c.MatchType != "doi" || c.Score != 1.0 {
		t.Errorf("cluster meta wrong: %+v", c)
	}
	got := clusterKeys(c)
	if got[0] != "A1" || got[1] != "A2" || len(got) != 2 {
		t.Errorf("members = %v, want [A1 A2]", got)
	}
}

func TestClusterByDOI_NormalizesCaseAndPrefix(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "", "10.1000/ABC"},
		[3]string{"A2", "", "https://doi.org/10.1000/abc"},
		[3]string{"A3", "", "  10.1000/ABC  "},
	)
	clusters := ClusterByDOI(cands)
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1 (case/prefix/whitespace should fold)", len(clusters))
	}
	if len(clusters[0].Members) != 3 {
		t.Errorf("cluster has %d members, want 3", len(clusters[0].Members))
	}
}

func TestClusterByDOI_EmptyDOIsIgnored(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "x", ""},
		[3]string{"A2", "x", ""},
	)
	if got := ClusterByDOI(cands); len(got) != 0 {
		t.Errorf("empty DOIs must not cluster, got %+v", got)
	}
}

func TestClusterByDOI_SingletonNotAClusterr(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "x", "10.1000/a"},
		[3]string{"A2", "y", "10.1000/b"},
	)
	if got := ClusterByDOI(cands); len(got) != 0 {
		t.Errorf("singletons must not form clusters, got %+v", got)
	}
}

func TestClusterByTitle_ExactNormalized(t *testing.T) {
	// Punctuation, case, and whitespace differences should all bucket.
	cands := makeCands(
		[3]string{"A1", "Deep Learning for Neuroimaging", ""},
		[3]string{"A2", "deep learning for neuroimaging", ""},
		[3]string{"A3", "Deep Learning: For Neuroimaging!", ""},
		[3]string{"A4", "Something else entirely different here", ""},
	)
	clusters := ClusterByTitle(cands, 0.85)
	// Expect one exact cluster of 3. A4 is alone → no fuzzy partner.
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(clusters))
	}
	c := clusters[0]
	if c.MatchType != "title-exact" || c.Score != 1.0 {
		t.Errorf("cluster meta wrong: %+v", c)
	}
	if len(c.Members) != 3 {
		t.Errorf("members = %d, want 3", len(c.Members))
	}
}

func TestClusterByTitle_FuzzyOnSingletons(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "a survey of graph neural networks for molecular property prediction", ""},
		[3]string{"A2", "a survery of graph neural networks for molecular property prediction", ""}, // typo
		[3]string{"A3", "totally unrelated work on protein folding dynamics in cells", ""},
	)
	clusters := ClusterByTitle(cands, 0.85)
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1", len(clusters))
	}
	c := clusters[0]
	if c.MatchType != "title-fuzzy" {
		t.Errorf("MatchType = %q, want title-fuzzy", c.MatchType)
	}
	if c.Score < 0.85 || c.Score >= 1.0 {
		t.Errorf("Score = %v, want in [0.85, 1.0)", c.Score)
	}
	keys := clusterKeys(c)
	if len(keys) != 2 || keys[0] != "A1" || keys[1] != "A2" {
		t.Errorf("members = %v, want [A1 A2]", keys)
	}
}

func TestClusterByTitle_ShortTitlesIgnoredByFuzzy(t *testing.T) {
	// Short titles must not match fuzzily — one-char edit on a 5-char
	// title is already an 80% ratio and would produce false positives.
	cands := makeCands(
		[3]string{"A1", "notes", ""},
		[3]string{"A2", "notez", ""},
	)
	if got := ClusterByTitle(cands, 0.85); len(got) != 0 {
		t.Errorf("short titles must not fuzzy-match, got %+v", got)
	}
}

func TestRunDuplicates_DedupAcrossStrategies(t *testing.T) {
	// A1 and A2 share both a DOI and a normalized title. They must appear
	// in exactly ONE cluster, not one per strategy. DOI wins because it's
	// the stronger signal.
	cands := makeCands(
		[3]string{"A1", "Deep Learning for Neuroimaging", "10.1000/foo"},
		[3]string{"A2", "deep learning for neuroimaging", "10.1000/foo"},
	)
	clusters := RunDuplicates(cands, DuplicatesOptions{
		Strategy:  StrategyBoth,
		Threshold: 0.85,
	})
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters, want 1 (deduped across DOI+title): %+v", len(clusters), clusters)
	}
	if clusters[0].MatchType != "doi" {
		t.Errorf("expected doi to win, got %q", clusters[0].MatchType)
	}
}

func TestRunDuplicates_StrategyFilter(t *testing.T) {
	cands := makeCands(
		[3]string{"A1", "paper x", "10.1000/foo"},
		[3]string{"A2", "paper x imported", "10.1000/foo"},
	)
	// DOI-only: finds the DOI match.
	if got := RunDuplicates(cands, DuplicatesOptions{Strategy: StrategyDOI, Threshold: 0.85}); len(got) != 1 || got[0].MatchType != "doi" {
		t.Errorf("StrategyDOI: got %+v", got)
	}
	// Title-only: titles differ too much (short + one word added) → no match.
	if got := RunDuplicates(cands, DuplicatesOptions{Strategy: StrategyTitle, Threshold: 0.85}); len(got) != 0 {
		t.Errorf("StrategyTitle should not match, got %+v", got)
	}
}

func TestClusterByTitle_ExactPreemptsFuzzy(t *testing.T) {
	// A1/A2 are exact-normalized matches; A3 is a typo of A1. The exact
	// cluster should absorb A1+A2 and A3 should remain unmatched (since
	// members of an exact cluster are not fuzzy-compared).
	cands := makeCands(
		[3]string{"A1", "deep learning for neuroimaging studies today", ""},
		[3]string{"A2", "Deep Learning for Neuroimaging Studies Today!", ""},
		[3]string{"A3", "deep learning for neuroimaging studeis today", ""}, // typo
	)
	clusters := ClusterByTitle(cands, 0.85)
	// We accept either:
	//   (a) one exact cluster of 2 (A3 orphaned), or
	//   (b) one exact cluster of 2 + one fuzzy cluster matching A3 to a representative.
	// Implementation picks (a) for simplicity — A3 doesn't get paired with itself.
	if len(clusters) < 1 {
		t.Fatalf("expected at least one cluster")
	}
	exactFound := false
	for _, c := range clusters {
		if c.MatchType == "title-exact" && len(c.Members) == 2 {
			exactFound = true
		}
	}
	if !exactFound {
		t.Errorf("expected exact cluster of 2, got %+v", clusters)
	}
}
