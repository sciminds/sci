package hygiene

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/doi"
	"github.com/sciminds/cli/internal/zot/local"
)

// SubobjectDOIStats summarizes a dois check run. Subobject is the count
// of items whose stored DOI matches a known publisher subobject pattern
// (Frontiers /abstract+/full, PLOS .tNNN/.gNNN/.sNNN, PNAS /-/DCSupplemental*).
type SubobjectDOIStats struct {
	Scanned   int `json:"scanned"`
	Subobject int `json:"subobject"`
}

// ScanSubobjectDOIs is the pure, DB-free entry point for the dois check.
// Feed it FieldValue rows scoped to the DOI field and it returns a Report
// with one Finding per item carrying a subobject DOI. Clean DOIs and
// rows for other fields pass through silently.
//
// Severity is SevWarn: the stored DOI may technically resolve via doi.org
// to the publisher's subobject page, but it points to a table/figure/
// supplement rather than the parent paper, so OpenAlex (and most other
// metadata APIs) 404 on it. The fix is to overwrite the stored DOI with
// the parent-paper form returned by doi.StripSubobject.
func ScanSubobjectDOIs(rows []local.FieldValue) *Report {
	findings := lo.FilterMap(rows, func(r local.FieldValue, _ int) (Finding, bool) {
		if r.Field != "DOI" {
			return Finding{}, false
		}
		if !doi.IsSubobject(r.Value) {
			return Finding{}, false
		}
		parent := doi.StripSubobject(r.Value)
		return Finding{
			Check:    "dois",
			Kind:     "subobject",
			ItemKey:  r.Key,
			Title:    r.Title,
			Severity: SevWarn,
			Message:  fmt.Sprintf("subobject DOI %q (suggest %q)", r.Value, parent),
			Fixable:  true,
		}, true
	})

	slices.SortFunc(findings, func(a, b Finding) int {
		return cmp.Compare(a.ItemKey, b.ItemKey)
	})

	return &Report{
		Check:    "dois",
		Scanned:  len(rows),
		Findings: findings,
		Stats: SubobjectDOIStats{
			Scanned:   len(rows),
			Subobject: len(findings),
		},
	}
}

// SubobjectDOIs is the DB-backed orchestrator: scans the DOI field for
// every content item and runs the pure check.
func SubobjectDOIs(db local.Reader) (*Report, error) {
	rows, err := db.ScanFieldValues([]string{"DOI"})
	if err != nil {
		return nil, err
	}
	return ScanSubobjectDOIs(rows), nil
}
