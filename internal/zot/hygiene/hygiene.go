// Package hygiene implements library-quality checks over a local Zotero
// database. Each check scans the read-only `local.DB` and returns a Report
// made up of Findings. The `zot doctor` command runs every check and merges
// their reports; individual `zot <check>` commands run one check in isolation.
//
// Checks in this package are pure — they observe, they never mutate. Any
// `--fix` behavior lives in the command layer, which dispatches to the write
// API under `internal/zot/api` based on Finding.Fixable.
package hygiene

// Severity ranks findings for display and filtering. Checks pick the level
// that matches the impact: Info for coverage gaps, Warn for dangling refs,
// Error for corruption-adjacent problems.
type Severity int

// Severity constants ranking findings by impact.
const (
	SevInfo Severity = iota
	SevWarn
	SevError
)

func (s Severity) String() string {
	switch s {
	case SevWarn:
		return "warn"
	case SevError:
		return "error"
	default:
		return "info"
	}
}

// Finding is one issue discovered by a hygiene check. Every check emits a
// slice of these so `zot doctor` can merge reports uniformly.
type Finding struct {
	Check    string   `json:"check"` // top-level check, e.g. "missing"
	Kind     string   `json:"kind"`  // sub-kind, e.g. "doi"
	ItemKey  string   `json:"item_key,omitempty"`
	Title    string   `json:"title,omitempty"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Fixable  bool     `json:"fixable"`
}

// Report is the result of running a single check. Stats is an optional
// per-check summary blob (e.g. coverage counts for `missing`) — its shape
// varies and is rendered by the corresponding Result type in internal/zot.
//
// Most checks emit Findings (one per item-level issue). Cluster-style
// checks like duplicates emit Clusters instead — the two fields are
// mutually informative, not exclusive, and doctor merges them uniformly.
type Report struct {
	Check    string    `json:"check"`
	Scanned  int       `json:"scanned"`
	Findings []Finding `json:"findings,omitempty"`
	Clusters []Cluster `json:"clusters,omitempty"`
	Stats    any       `json:"stats,omitempty"`
}

// CountBySeverity returns how many findings in the report fall into each
// severity bucket. Useful for doctor-style summary lines.
func (r *Report) CountBySeverity() map[Severity]int {
	out := map[Severity]int{}
	for _, f := range r.Findings {
		out[f.Severity]++
	}
	return out
}
