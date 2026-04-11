package hygiene

import (
	"os"
	"testing"
)

func TestParseOrphanKind(t *testing.T) {
	t.Parallel()
	for _, k := range AllOrphanKinds {
		got, err := ParseOrphanKind(string(k))
		if err != nil {
			t.Errorf("ParseOrphanKind(%q) error: %v", k, err)
		}
		if got != k {
			t.Errorf("ParseOrphanKind(%q) = %q, want %q", k, got, k)
		}
	}
	if _, err := ParseOrphanKind("bogus"); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestSeverityForOrphan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind OrphanKind
		want Severity
	}{
		{OrphanEmptyCollection, SevInfo},
		{OrphanStandaloneAttachment, SevWarn},
		{OrphanStandaloneNote, SevInfo},
		{OrphanUncollectedItem, SevInfo},
		{OrphanUnusedTag, SevInfo},
		{OrphanMissingFile, SevError},
	}
	for _, c := range cases {
		if got := severityForOrphan(c.kind); got != c.want {
			t.Errorf("severityForOrphan(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestOrphans_RealLibrary(t *testing.T) {
	t.Parallel()
	if os.Getenv("SLOW") == "" {
		t.Skip("set SLOW=1 to run real-library orphans scan")
	}
	db := openRealDB(t)
	rep, err := Orphans(db, OrphansOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := rep.Stats.(OrphansStats)
	if !ok {
		t.Fatalf("stats is %T, want OrphansStats", rep.Stats)
	}

	// Invariants: no Fatal, every finding severity matches its kind.
	for _, f := range rep.Findings {
		want := severityForOrphan(OrphanKind(f.Kind))
		if f.Severity != want {
			t.Errorf("finding %s: severity = %v, want %v", f.Kind, f.Severity, want)
		}
	}

	t.Logf("total=%d", stats.Total)
	for _, k := range AllOrphanKinds {
		if n, ok := stats.CountsByKind[string(k)]; ok {
			t.Logf("  %-24s %d", k, n)
		}
	}
}

func TestOrphansKindsSelected(t *testing.T) {
	t.Parallel()
	// Nil selection = default kinds (excludes uncollected-item and
	// missing-file). Subset selection = exactly those kinds.
	def := orphanKindsSelected(nil)
	if len(def) != len(defaultOrphanKinds) {
		t.Errorf("nil kinds = %d, want default %d", len(def), len(defaultOrphanKinds))
	}
	// uncollected-item and missing-file must be EXCLUDED from default.
	if _, ok := def[OrphanUncollectedItem]; ok {
		t.Error("uncollected-item should not be in default selection")
	}
	if _, ok := def[OrphanMissingFile]; ok {
		t.Error("missing-file should not be in default selection")
	}

	// Explicit selection overrides the default set.
	sub := orphanKindsSelected([]OrphanKind{OrphanUnusedTag, OrphanUncollectedItem})
	if len(sub) != 2 {
		t.Errorf("subset len = %d, want 2", len(sub))
	}
	if _, ok := sub[OrphanUnusedTag]; !ok {
		t.Error("OrphanUnusedTag not in selection")
	}
	if _, ok := sub[OrphanUncollectedItem]; !ok {
		t.Error("explicit uncollected-item should be in selection")
	}
	if _, ok := sub[OrphanStandaloneNote]; ok {
		t.Error("OrphanStandaloneNote should not be in selection")
	}
}
