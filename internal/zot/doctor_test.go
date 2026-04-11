package zot

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/hygiene"
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
	for i := 0; i < errors; i++ {
		r.Findings = append(r.Findings, hygiene.Finding{Check: check, Severity: hygiene.SevError})
	}
	for i := 0; i < warns; i++ {
		r.Findings = append(r.Findings, hygiene.Finding{Check: check, Severity: hygiene.SevWarn})
	}
	for i := 0; i < infos; i++ {
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
	if !strings.Contains(out, "zot invalid") {
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
