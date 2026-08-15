package zot

import (
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot/hygiene"
)

func TestParseDoctorCheck(t *testing.T) {
	t.Parallel()
	for _, want := range DoctorChecks {
		got, err := ParseDoctorCheck(want)
		if err != nil {
			t.Errorf("ParseDoctorCheck(%q) returned error: %v", want, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDoctorCheck(%q) = %q", want, got)
		}
	}
	if _, err := ParseDoctorCheck("bogus"); err == nil {
		t.Errorf("ParseDoctorCheck(bogus) should fail")
	}
	// Tolerant of whitespace.
	if _, err := ParseDoctorCheck("  missing  "); err != nil {
		t.Errorf("whitespace should be trimmed: %v", err)
	}
}

func TestSelectedChecks_Defaults(t *testing.T) {
	t.Parallel()
	got := selectedChecks(nil)
	for _, c := range DoctorChecks {
		if !got[c] {
			t.Errorf("empty input must select %q", c)
		}
	}

	got = selectedChecks([]string{"missing", "invalid"})
	if !got["missing"] || !got["invalid"] || got["orphans"] || got["duplicates"] {
		t.Errorf("subset selection wrong: %+v", got)
	}
}

// buildReport is a tiny helper for building a hygiene.Report with a known
// severity mix, so the doctor aggregation tests don't need a database.
func buildReport(check string, errors, warns, infos int) *hygiene.Report {
	r := &hygiene.Report{Check: check, Scanned: 100}
	for range errors {
		r.Findings = append(r.Findings, hygiene.Finding{Check: check, Severity: hygiene.SevError})
	}
	for range warns {
		r.Findings = append(r.Findings, hygiene.Finding{Check: check, Severity: hygiene.SevWarn})
	}
	for range infos {
		r.Findings = append(r.Findings, hygiene.Finding{Check: check, Severity: hygiene.SevInfo})
	}
	return r
}

func TestDoctorResult_TotalsAndRendering(t *testing.T) {
	t.Parallel()
	res := &DoctorResult{
		Scanned: 100,
		Reports: map[string]*hygiene.Report{
			"invalid": buildReport("invalid", 2, 3, 0),
			"missing": buildReport("missing", 0, 5, 7),
			"orphans": buildReport("orphans", 0, 1, 0),
			"duplicates": {
				Check:   "duplicates",
				Scanned: 100,
				Clusters: []hygiene.Cluster{
					{Check: "duplicates", MatchType: "doi", Score: 1.0},
					{Check: "duplicates", MatchType: "doi", Score: 1.0},
				},
				Stats: hygiene.DuplicatesStats{
					Scanned:       100,
					Strategy:      "both",
					Fuzzy:         false,
					ClusterCount:  2,
					ItemsInGroups: 4,
				},
			},
		},
		Order: []string{"invalid", "missing", "orphans", "duplicates"},
	}
	// Pretend the orchestrator rolled these up.
	for _, name := range res.Order {
		c := res.Reports[name].CountBySeverity()
		res.Totals.Errors += c[hygiene.SevError]
		res.Totals.Warnings += c[hygiene.SevWarn]
		res.Totals.Info += c[hygiene.SevInfo]
		res.Totals.Clusters += len(res.Reports[name].Clusters)
	}

	if res.Totals.Errors != 2 {
		t.Errorf("errors = %d, want 2", res.Totals.Errors)
	}
	if res.Totals.Warnings != 9 {
		t.Errorf("warnings = %d, want 9", res.Totals.Warnings)
	}
	if res.Totals.Info != 7 {
		t.Errorf("info = %d, want 7", res.Totals.Info)
	}
	if res.Totals.Clusters != 2 {
		t.Errorf("clusters = %d, want 2", res.Totals.Clusters)
	}

	out := res.Human()
	for _, want := range []string{"Library Health", "invalid", "missing", "orphans", "duplicates", "2 cluster"} {
		if !strings.Contains(out, want) {
			t.Errorf("Human() missing %q:\n%s", want, out)
		}
	}
	// Must NOT print the healthy-library footer when there are findings.
	if strings.Contains(out, "library looks healthy") {
		t.Errorf("Human() should not claim healthy:\n%s", out)
	}
	// Must point at the per-check commands for drilldown.
	if !strings.Contains(out, "sci zot doctor invalid") {
		t.Errorf("Human() should point at per-check commands:\n%s", out)
	}
}

func TestDoctorResult_HealthyLibrary(t *testing.T) {
	t.Parallel()
	res := &DoctorResult{
		Scanned: 100,
		Reports: map[string]*hygiene.Report{
			"invalid": buildReport("invalid", 0, 0, 0),
			"missing": buildReport("missing", 0, 0, 0),
			"orphans": buildReport("orphans", 0, 0, 0),
			"duplicates": {
				Check:   "duplicates",
				Scanned: 100,
				Stats: hygiene.DuplicatesStats{
					Scanned: 100, Strategy: "both", ClusterCount: 0,
				},
			},
		},
		Order: []string{"invalid", "missing", "orphans", "duplicates"},
	}
	out := res.Human()
	if !strings.Contains(out, "library looks healthy") {
		t.Errorf("clean library should show healthy footer:\n%s", out)
	}
}

// When a totals bucket is zero, the footer should swap the warn/fail
// glyph for the OK checkmark and render the count in the pass color so
// "0 error" reads as a positive signal instead of a scary red row.
func TestDoctorResult_FooterZeroBucketsRenderAsPass(t *testing.T) {
	t.Parallel()
	res := &DoctorResult{
		Scanned: 100,
		Reports: map[string]*hygiene.Report{
			"invalid":    buildReport("invalid", 0, 3, 0),
			"missing":    buildReport("missing", 0, 0, 4),
			"orphans":    buildReport("orphans", 0, 0, 0),
			"duplicates": {Check: "duplicates", Scanned: 100, Stats: hygiene.DuplicatesStats{Scanned: 100}},
		},
		Order: []string{"invalid", "missing", "orphans", "duplicates"},
		Totals: DoctorTotals{
			Errors: 0, Warnings: 3, Info: 4,
		},
	}
	out := res.Human()

	wantPassZero := uikit.TUI.Pass().Render("0 error")
	if !strings.Contains(out, wantPassZero) {
		t.Errorf("Human() should render '0 error' in the pass color:\n%s", out)
	}
	wantWarnNonzero := uikit.TUI.Warn().Render("3 warn")
	if !strings.Contains(out, wantWarnNonzero) {
		t.Errorf("Human() should still render nonzero warnings in the warn color:\n%s", out)
	}

	// And when warnings are zero too: both buckets should be pass-colored.
	res.Totals.Warnings = 0
	for _, name := range []string{"invalid", "missing", "orphans"} {
		res.Reports[name] = buildReport(name, 0, 0, 0)
	}
	out = res.Human()
	if !strings.Contains(out, uikit.TUI.Pass().Render("0 warn")) {
		t.Errorf("Human() should render '0 warn' in the pass color when warnings=0:\n%s", out)
	}
}

func TestDoctorResult_DeepLabel(t *testing.T) {
	t.Parallel()
	res := &DoctorResult{
		Reports: map[string]*hygiene.Report{},
		Deep:    true,
	}
	if !strings.Contains(res.Human(), "deep mode") {
		t.Errorf("Human() should mark deep mode")
	}
	res.Deep = false
	if !strings.Contains(res.Human(), "fast mode") {
		t.Errorf("Human() should mark fast mode")
	}
}

// TestSubobjectDOIsResult_FooterNamesNoRemovedFlag pins the human footer of
// `sci zot doctor dois`. The check reports and stops, so the footer must not
// hand back a command line at all: `--fix` is not a flag this command
// accepts, and no other binary is a destination. Naming either contradicts
// the command's own Description and gives the reader something that errors.
func TestSubobjectDOIsResult_FooterNamesNoRemovedFlag(t *testing.T) {
	t.Parallel()
	res := SubobjectDOIsResult{Report: &hygiene.Report{
		Check:   "dois",
		Scanned: 2,
		Findings: []hygiene.Finding{{
			Check: "dois", Kind: "subobject", ItemKey: "ABC12345",
			Title: "A paper", Severity: hygiene.SevWarn,
			Message: "DOI points at a figure, not the paper",
		}},
	}}
	out := res.Human()
	for _, banned := range []string{"--fix", "--apply"} {
		if strings.Contains(out, banned) {
			t.Errorf("footer advertises %s, which the command no longer accepts:\n%s", banned, out)
		}
	}
	for _, banned := range []string{"zot fix", "zot binary"} {
		if strings.Contains(out, banned) {
			t.Errorf("footer names %q, which is not a command this surface has:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "1 finding(s)") {
		t.Errorf("footer should report the finding count:\n%s", out)
	}
}

// C1: the dashboard headline claimed "14962 items scanned" on a 5,494-item
// library. The aggregate took the max Scanned across every report, but the
// checks do not all count the same thing — `invalid` counts field values
// (several per item) and `orphans` counts what it FOUND. A max over
// incomparable units, printed under one "items" label, is a wrong number.
func TestDoctor_AggregateCountsItemsOnly(t *testing.T) {
	t.Parallel()
	items := &hygiene.Report{Check: "missing", Scanned: 5494, Unit: hygiene.UnitItems}
	fields := &hygiene.Report{Check: "invalid", Scanned: 14962, Unit: hygiene.UnitFieldValues}
	found := &hygiene.Report{Check: "orphans", Scanned: 1, Unit: hygiene.UnitFindings}

	res := &DoctorResult{Reports: map[string]*hygiene.Report{}}
	for _, rep := range []*hygiene.Report{fields, items, found} {
		res.absorb(rep)
	}
	if res.Scanned != 5494 {
		t.Errorf("Scanned = %d, want the item count 5494 (field values and findings are other units)", res.Scanned)
	}
	if got := stripStyle(res.Human()); !strings.Contains(got, "5494 items scanned") {
		t.Errorf("human headline does not report the item count:\n%s", got)
	}
	if got := stripStyle(res.Human()); strings.Contains(got, "14962") {
		t.Errorf("human headline still reports a field-value count as items:\n%s", got)
	}
}

// A run with no item-scoped check has no item count to report, and must not
// invent one — `doctor --check invalid` scans field values only.
func TestDoctor_NoItemScopedCheckReportsChecksInstead(t *testing.T) {
	t.Parallel()
	res := &DoctorResult{Reports: map[string]*hygiene.Report{}}
	res.absorb(&hygiene.Report{Check: "invalid", Scanned: 14962, Unit: hygiene.UnitFieldValues})

	got := stripStyle(res.Human())
	if strings.Contains(got, "items scanned") {
		t.Errorf("headline claims an item count no check measured:\n%s", got)
	}
	if !strings.Contains(got, "1 check(s) run") {
		t.Errorf("headline does not say what actually ran:\n%s", got)
	}
}

// stripStyle drops ANSI escapes so headline assertions don't depend on
// terminal styling.
func stripStyle(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
