package hygiene

import (
	"cmp"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/citekey"
	"github.com/sciminds/cli/internal/zot/local"
)

// CitekeysStats summarizes a citekeys check run. Non-Canonical is deliberately
// a separate bucket from Invalid: BBT-managed libraries produce 100%
// non-canonical keys by design and the doctor summary should not treat
// that as an error-level signal. Collisions are counted at the *item*
// level — if three items share one key, Collisions increments by 3, not
// by 1, so the aggregate matches the number of findings.
type CitekeysStats struct {
	Scanned      int `json:"scanned"`
	Stored       int `json:"stored"`        // items with a stored cite-key (native or BBT extra)
	Unstored     int `json:"unstored"`      // items with no stored cite-key — fix would synthesize
	Valid        int `json:"valid"`         // stored keys matching v2 spec
	NonCanonical int `json:"non_canonical"` // stored but doesn't match v2 spec
	Invalid      int `json:"invalid"`       // structurally broken (whitespace, BibTeX-illegal chars)
	Collisions   int `json:"collisions"`    // items sharing a cite-key with another item
}

// CitekeysFromRows is the pure, DB-free entry point for the citekeys check.
// Feed it CiteKeyRow tuples (from local.DB.ScanCiteKeys or hand-built in
// tests) and it returns a Report with one Finding per problematic item.
//
// Resolution order mirrors citekey.Resolve: native `citationKey` field
// first, then legacy BBT `Citation Key:` line parsed out of `extra`. Rows
// with neither are counted as Unstored and do NOT emit a finding —
// synthesis at export time will produce a canonical key for them on
// demand, so there's nothing to flag read-only. (The future fix command
// is where we'll materialize those into the Zotero `citationKey` field.)
func CitekeysFromRows(rows []local.CiteKeyRow) *Report {
	stats := CitekeysStats{Scanned: len(rows)}
	findings := make([]Finding, 0)

	// First pass: resolve → validate → count → emit per-row findings.
	// We also build a reverse index to detect collisions in a second
	// pass; resolving twice would double the regex cost on large libs.
	type resolved struct {
		row local.CiteKeyRow
		key string
	}
	resolvedRows := make([]resolved, 0, len(rows))
	keyToItems := map[string][]string{} // cite-key → []zotero item key

	for _, r := range rows {
		key := r.CitationKey
		if key == "" {
			key = citekey.FromExtra(r.Extra)
		}
		if key == "" {
			stats.Unstored++
			continue
		}
		stats.Stored++
		resolvedRows = append(resolvedRows, resolved{row: r, key: key})
		keyToItems[key] = append(keyToItems[key], r.Key)

		switch st, reason := citekey.Validate(key); st {
		case citekey.Valid:
			stats.Valid++
		case citekey.NonCanonical:
			stats.NonCanonical++
			findings = append(findings, Finding{
				Check:    "citekeys",
				Kind:     "non-canonical",
				ItemKey:  r.Key,
				Title:    r.Title,
				Severity: SevWarn,
				Message:  "cite-key " + quote(key) + ": " + reason,
				Fixable:  true,
			})
		case citekey.Invalid:
			stats.Invalid++
			findings = append(findings, Finding{
				Check:    "citekeys",
				Kind:     "invalid",
				ItemKey:  r.Key,
				Title:    r.Title,
				Severity: SevError,
				Message:  "cite-key " + quote(key) + ": " + reason,
				Fixable:  true,
			})
		}
	}

	// Second pass: every item whose cite-key is shared with *any* other
	// item gets a collision finding. We emit one finding per participating
	// item (not one per cluster) so the per-item drill-in surfaces the
	// problem from both sides, matching how `invalid` and `missing` work.
	for _, rr := range resolvedRows {
		mates := keyToItems[rr.key]
		if len(mates) < 2 {
			continue
		}
		stats.Collisions++
		findings = append(findings, Finding{
			Check:    "citekeys",
			Kind:     "collision",
			ItemKey:  rr.row.Key,
			Title:    rr.row.Title,
			Severity: SevError,
			Message:  "cite-key " + quote(rr.key) + " is shared by " + joinKeys(mates, rr.row.Key),
			Fixable:  true,
		})
	}

	// Stable ordering: by item key, then by finding kind, so golden-ish
	// tests and human output are deterministic.
	slices.SortFunc(findings, func(a, b Finding) int {
		if c := cmp.Compare(a.ItemKey, b.ItemKey); c != 0 {
			return c
		}
		return cmp.Compare(a.Kind, b.Kind)
	})

	return &Report{
		Check:    "citekeys",
		Scanned:  len(rows),
		Findings: findings,
		Stats:    stats,
	}
}

// Citekeys is the DB-backed orchestrator. Scans every content item's
// stored cite-key fields and runs the pure check.
func Citekeys(db local.Reader) (*Report, error) {
	rows, err := db.ScanCiteKeys()
	if err != nil {
		return nil, err
	}
	return CitekeysFromRows(rows), nil
}

// joinKeys formats the "shared by" list for a collision message. Skips
// the current item so the rendered message reads like "shared by X, Y"
// rather than "shared by SELF, X, Y".
func joinKeys(all []string, self string) string {
	return strings.Join(lo.Without(all, self), ", ")
}

// quote wraps a cite-key in backticks for display. Avoids the ambiguity
// of quoting inside a message that itself might get quoted upstream.
func quote(s string) string { return "`" + s + "`" }
