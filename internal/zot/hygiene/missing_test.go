package hygiene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

// findRepoRoot walks upward from the test binary's working directory until
// it finds a go.mod. Used by the real-db smoke test so it works regardless
// of which package subdirectory `go test` is invoked from.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from cwd")
		}
		dir = parent
	}
}

// openRealDB opens sci-go/zotero.sqlite if it's present. The file is
// gitignored and not shipped with the repo; the test skips when absent so
// CI stays green.
func openRealDB(t *testing.T) *local.DB {
	t.Helper()
	root := findRepoRoot(t)
	path := filepath.Join(root, "zotero.sqlite")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no ./zotero.sqlite at repo root — skipping real-db test")
	}
	db, err := local.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMissing_RealLibrary(t *testing.T) {
	// Gated behind SLOW=1 — the SQL itself is covered by the local
	// package fixture test; this one is for eyeballing coverage numbers
	// against a real library and catches ~nothing that the fixture misses.
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library missing scan")
	}
	db := openRealDB(t)

	rep, err := Missing(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned == 0 {
		t.Fatal("expected a non-empty real library")
	}
	stats, ok := rep.Stats.(MissingStats)
	if !ok {
		t.Fatalf("stats is %T, want MissingStats", rep.Stats)
	}
	if len(stats.Coverage) != len(AllMissingFields) {
		t.Errorf("coverage has %d rows, want %d", len(stats.Coverage), len(AllMissingFields))
	}
	for _, c := range stats.Coverage {
		if c.Present+c.Missing != stats.Scanned {
			t.Errorf("field %s: present(%d)+missing(%d) != scanned(%d)",
				c.Field, c.Present, c.Missing, stats.Scanned)
		}
		if c.PercentPresent < 0 || c.PercentPresent > 100 {
			t.Errorf("field %s: percent %.2f out of range", c.Field, c.PercentPresent)
		}
	}
	t.Logf("scanned=%d", stats.Scanned)
	for _, c := range stats.Coverage {
		t.Logf("  %-10s %5d / %-5d  %5.1f%%",
			c.Field, c.Present, stats.Scanned, c.PercentPresent)
	}

	// Subset selection should shrink the coverage row set.
	rep2, err := Missing(db, []MissingField{FieldDOI, FieldAbstract})
	if err != nil {
		t.Fatal(err)
	}
	s2 := rep2.Stats.(MissingStats)
	if len(s2.Coverage) != 2 {
		t.Errorf("subset coverage has %d rows, want 2", len(s2.Coverage))
	}
}

func TestDuplicates_RealLibrary(t *testing.T) {
	// ~26s on a 5k-item library — gated behind SLOW=1 to match the
	// proj/new integration-test convention noted in CLAUDE.md. The
	// smaller TestMissing_RealLibrary above runs unconditionally.
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library duplicate scan")
	}
	db := openRealDB(t)
	rep, err := Duplicates(db, DuplicatesOptions{
		Strategy:  StrategyBoth,
		Fuzzy:     true,
		Threshold: 0.85,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned == 0 {
		t.Fatal("expected a non-empty real library")
	}
	stats, ok := rep.Stats.(DuplicatesStats)
	if !ok {
		t.Fatalf("stats is %T, want DuplicatesStats", rep.Stats)
	}
	if stats.ClusterCount != len(rep.Clusters) {
		t.Errorf("cluster_count=%d but len(Clusters)=%d", stats.ClusterCount, len(rep.Clusters))
	}

	// Invariants: every cluster must have ≥2 members, a known match type,
	// and a score in [0,1]. Log a few examples for eyeballing.
	validTypes := map[string]bool{"doi": true, "title-exact": true, "title-fuzzy": true}
	for _, c := range rep.Clusters {
		if len(c.Members) < 2 {
			t.Errorf("cluster with <2 members: %+v", c)
		}
		if !validTypes[c.MatchType] {
			t.Errorf("unknown match type %q", c.MatchType)
		}
		if c.Score < 0 || c.Score > 1 {
			t.Errorf("score %v out of [0,1]", c.Score)
		}
	}

	t.Logf("scanned=%d clusters=%d items_in_groups=%d",
		stats.Scanned, stats.ClusterCount, stats.ItemsInGroups)
	// Log the first few for visual sanity.
	for i, c := range rep.Clusters {
		if i >= 5 {
			break
		}
		keys := make([]string, len(c.Members))
		for j, m := range c.Members {
			keys[j] = m.Key
		}
		t.Logf("  [%s] score=%.2f keys=%v", c.MatchType, c.Score, keys)
	}
}

func TestSeverityFor(t *testing.T) {
	cases := []struct {
		field MissingField
		want  Severity
	}{
		{FieldTitle, SevError},
		{FieldCreators, SevWarn},
		{FieldDate, SevWarn},
		{FieldDOI, SevInfo},
		{FieldAbstract, SevInfo},
		{FieldURL, SevInfo},
		{FieldPDF, SevInfo},
		{FieldTags, SevInfo},
	}
	for _, c := range cases {
		if got := severityFor(c.field); got != c.want {
			t.Errorf("severityFor(%q) = %v, want %v", c.field, got, c.want)
		}
	}
}

func TestParseMissingField(t *testing.T) {
	for _, f := range AllMissingFields {
		got, err := ParseMissingField(string(f))
		if err != nil {
			t.Errorf("ParseMissingField(%q) error: %v", f, err)
		}
		if got != f {
			t.Errorf("ParseMissingField(%q) = %q, want %q", f, got, f)
		}
	}
	if _, err := ParseMissingField("bogus"); err == nil {
		t.Error("expected error for unknown field")
	}
}
