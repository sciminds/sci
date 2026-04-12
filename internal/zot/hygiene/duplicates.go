package hygiene

import (
	"cmp"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/zot/local"
)

// DuplicateCandidate is the shape the duplicate clusterers operate on.
// Re-exported from package local so the clusterers (pure functions) and
// the SQL scan share a single definition — the clusterers accept
// constructed values just as happily as the SQL results.
type DuplicateCandidate = local.DuplicateCandidate

// ClusterMember is one entry within a Cluster. It carries enough context
// for the user to decide which record to keep when resolving a duplicate.
type ClusterMember struct {
	Key      string `json:"key"`
	Title    string `json:"title,omitempty"`
	Date     string `json:"date,omitempty"`
	DOI      string `json:"doi,omitempty"`
	PDFCount int    `json:"pdf_count"`
}

// Cluster is a group of items the check believes refer to the same work.
// MatchType records how the match was made ("doi", "title-exact",
// "title-fuzzy") and Score is the confidence in [0,1]. A Score of 1.0
// means exact agreement on the match key.
type Cluster struct {
	Check     string          `json:"check"`
	MatchType string          `json:"match_type"`
	Score     float64         `json:"score"`
	Members   []ClusterMember `json:"members"`
}

// Strategy selects which pass(es) the duplicate check runs.
type Strategy string

const (
	StrategyDOI   Strategy = "doi"
	StrategyTitle Strategy = "title"
	StrategyBoth  Strategy = "both"
)

// DuplicatesOptions configures RunDuplicates and Duplicates.
//
// Strategy picks *what* to match on (DOI, normalized title, or both). It
// does NOT control whether the fuzzy title pass runs — that's Fuzzy.
//
// Fuzzy toggles the slow second pass of the title clusterer: a length-
// windowed Levenshtein comparison over singletons left over from the
// exact-normalized bucketing. It's off by default because on a 5k-item
// library it takes ~30s, whereas the DOI + exact-title passes finish in
// under a second. `zot doctor duplicates --fuzzy` (and `zot doctor --deep`)
// enable it.
//
// Threshold is the minimum SimilarityRatio (in [0,1]) the fuzzy pass
// uses to pair singletons. Only consulted when Fuzzy is true. A typical
// value is 0.85.
type DuplicatesOptions struct {
	Strategy  Strategy
	Fuzzy     bool
	Threshold float64
}

// RunDuplicates is the pure, DB-free entry point. It runs the configured
// strategies over the candidate slice and merges the resulting clusters
// into a single list, preferring DOI matches over title matches when both
// produce a cluster containing the same member set.
//
// Test-facing; the Duplicates() orchestrator wraps this with a DB scan.
func RunDuplicates(cands []DuplicateCandidate, opts DuplicatesOptions) []Cluster {
	if opts.Strategy == "" {
		opts.Strategy = StrategyBoth
	}
	if opts.Threshold == 0 {
		opts.Threshold = 0.85
	}

	var doiClusters, titleClusters []Cluster
	if opts.Strategy == StrategyDOI || opts.Strategy == StrategyBoth {
		doiClusters = ClusterByDOI(cands)
	}
	if opts.Strategy == StrategyTitle || opts.Strategy == StrategyBoth {
		titleClusters = ClusterByTitle(cands, opts.Threshold, opts.Fuzzy)
	}

	// Track which items are already captured by a DOI cluster so title
	// clusters don't re-emit overlapping membership. DOI is the stronger
	// signal — when both strategies agree, we keep the DOI cluster and
	// drop any title cluster that overlaps it.
	claimed := map[string]bool{}
	for _, c := range doiClusters {
		for _, m := range c.Members {
			claimed[m.Key] = true
		}
	}

	out := doiClusters
	for _, c := range titleClusters {
		overlap := false
		for _, m := range c.Members {
			if claimed[m.Key] {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		for _, m := range c.Members {
			claimed[m.Key] = true
		}
		out = append(out, c)
	}
	return out
}

// Duplicates is the DB-backed orchestrator. It scans the local library
// for candidates, runs RunDuplicates with the given options, and wraps
// the result in a hygiene.Report.
func Duplicates(db local.Reader, opts DuplicatesOptions) (*Report, error) {
	cands, err := db.ScanDuplicateCandidates()
	if err != nil {
		return nil, err
	}
	clusters := RunDuplicates(cands, opts)
	return &Report{
		Check:   "duplicates",
		Scanned: len(cands),
		Stats: DuplicatesStats{
			Scanned:       len(cands),
			Strategy:      string(opts.Strategy),
			Fuzzy:         opts.Fuzzy,
			Threshold:     opts.Threshold,
			ClusterCount:  len(clusters),
			ItemsInGroups: countClusterMembers(clusters),
		},
		Clusters: clusters,
	}, nil
}

// DuplicatesStats is the summary shape attached to Report.Stats for
// duplicate runs. Mirrors MissingStats in spirit — the renderer reads
// this instead of recounting the clusters slice.
type DuplicatesStats struct {
	Scanned       int     `json:"scanned"`
	Strategy      string  `json:"strategy"`
	Fuzzy         bool    `json:"fuzzy"`
	Threshold     float64 `json:"threshold"`
	ClusterCount  int     `json:"cluster_count"`
	ItemsInGroups int     `json:"items_in_groups"`
}

func countClusterMembers(cs []Cluster) int {
	n := 0
	for _, c := range cs {
		n += len(c.Members)
	}
	return n
}

// ClusterByDOI groups candidates whose normalized DOI is identical. DOI
// normalization: trim whitespace, lowercase, strip common URL prefixes.
// Candidates with empty DOIs are skipped; groups of size < 2 are dropped.
//
// Member order inside a cluster is the input order (stable). Cluster
// order across the return slice is sorted by the DOI key for determinism.
func ClusterByDOI(cands []DuplicateCandidate) []Cluster {
	buckets := map[string][]DuplicateCandidate{}
	for _, c := range cands {
		key := normalizeDOI(c.DOI)
		if key == "" {
			continue
		}
		buckets[key] = append(buckets[key], c)
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var out []Cluster
	for _, k := range keys {
		members := buckets[k]
		if len(members) < 2 {
			continue
		}
		out = append(out, Cluster{
			Check:     "duplicates",
			MatchType: "doi",
			Score:     1.0,
			Members:   toMembers(members),
		})
	}
	return out
}

// normalizeDOI folds superficial differences (whitespace, case, URL
// prefixes) so that the same DOI stored three different ways still buckets
// together. Empty or unrecognized input yields an empty key and the caller
// skips it.
func normalizeDOI(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	for _, prefix := range []string{
		"https://doi.org/",
		"http://doi.org/",
		"https://dx.doi.org/",
		"http://dx.doi.org/",
		"doi:",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	return strings.TrimSpace(s)
}

// minFuzzyTitleLen is the normalized-length floor below which we refuse to
// run the fuzzy matcher. Short titles yield high Levenshtein ratios on
// trivial differences ("notes" vs "notez" = 0.80), which generates noise.
// 20 runes covers the practical minimum of a real paper title.
const minFuzzyTitleLen = 20

// ClusterByTitle groups candidates in up to two passes. First, it buckets
// by normalized title and emits any bucket with ≥2 members as a
// "title-exact" cluster (Score 1.0). Second, if fuzzy is true, it runs a
// slow fuzzy pass over the *singletons* from the first pass, pairing
// titles whose SimilarityRatio meets threshold — emitted as "title-fuzzy"
// clusters with the pair's actual ratio as the Score.
//
// The exact pass is a hash-bucket operation and is effectively free. The
// fuzzy pass is O(n²) Levenshtein over the singletons and takes tens of
// seconds on real libraries; callers opt in via the fuzzy flag. The
// fuzzy pass also skips titles shorter than minFuzzyTitleLen to avoid
// false positives on stub entries.
//
// Only exact clusters are transitive; fuzzy pairs are emitted as a chain
// (A~B and B~C both pass → cluster {A,B,C}), but A is only compared once
// per round so the cost stays O(n^2) on the singleton population.
func ClusterByTitle(cands []DuplicateCandidate, threshold float64, fuzzy bool) []Cluster {
	// Pass 1: bucket by normalized title.
	type normEntry struct {
		cand DuplicateCandidate
		norm string
	}
	entries := make([]normEntry, 0, len(cands))
	buckets := map[string][]int{} // norm → indices into entries
	for _, c := range cands {
		norm := NormalizeTitle(c.Title)
		if norm == "" {
			continue
		}
		idx := len(entries)
		entries = append(entries, normEntry{cand: c, norm: norm})
		buckets[norm] = append(buckets[norm], idx)
	}

	// Deterministic iteration order for exact clusters.
	bucketKeys := make([]string, 0, len(buckets))
	for k := range buckets {
		bucketKeys = append(bucketKeys, k)
	}
	slices.Sort(bucketKeys)

	var out []Cluster
	exactMembers := map[int]bool{} // entry indices already captured by an exact cluster
	for _, k := range bucketKeys {
		idxs := buckets[k]
		if len(idxs) < 2 {
			continue
		}
		members := make([]DuplicateCandidate, 0, len(idxs))
		for _, i := range idxs {
			members = append(members, entries[i].cand)
			exactMembers[i] = true
		}
		out = append(out, Cluster{
			Check:     "duplicates",
			MatchType: "title-exact",
			Score:     1.0,
			Members:   toMembers(members),
		})
	}

	if !fuzzy {
		return out
	}

	// Pass 2: fuzzy match over singletons.
	//
	// Length-prefiltered comparison: precompute rune-length of each
	// normalized title, sort by length, then for each item only scan
	// forward while the length delta could still admit a match. The
	// inner loop also uses levenshteinCapped so individual comparisons
	// abort early when the row-minimum overshoots the threshold budget.
	type fuzzyItem struct {
		entryIdx int
		runes    []rune
		length   int
	}
	fuzz := make([]fuzzyItem, 0, len(entries))
	for i := range entries {
		if exactMembers[i] {
			continue
		}
		runes := []rune(entries[i].norm)
		if len(runes) < minFuzzyTitleLen {
			continue
		}
		fuzz = append(fuzz, fuzzyItem{entryIdx: i, runes: runes, length: len(runes)})
	}
	slices.SortFunc(fuzz, func(a, b fuzzyItem) int { return cmp.Compare(a.length, b.length) })

	claimed := map[int]bool{}
	for ai := range fuzz {
		iIdx := fuzz[ai].entryIdx
		if claimed[iIdx] {
			continue
		}
		la := fuzz[ai].length
		// For a ratio ≥ threshold we need dist ≤ (1-threshold)*maxLen.
		// Sorting ascending means lb ≥ la in the inner loop, so the
		// maximum permissible length for lb is la / threshold, and the
		// distance budget is computed against maxLen = lb.
		maxLenB := int(float64(la)/threshold + 0.5)

		cluster := []int{iIdx}
		bestScore := 0.0
		for bi := ai + 1; bi < len(fuzz); bi++ {
			lb := fuzz[bi].length
			if lb > maxLenB {
				break // length-sorted: no further candidates can match
			}
			jIdx := fuzz[bi].entryIdx
			if claimed[jIdx] {
				continue
			}
			maxLen := lb // lb ≥ la by sort order
			budget := int(float64(maxLen) * (1 - threshold))
			d := levenshteinCapped(fuzz[ai].runes, fuzz[bi].runes, budget)
			if d > budget {
				continue
			}
			r := 1.0 - float64(d)/float64(maxLen)
			if r >= threshold {
				cluster = append(cluster, jIdx)
				claimed[jIdx] = true
				if r > bestScore {
					bestScore = r
				}
			}
		}
		if len(cluster) < 2 {
			continue
		}
		claimed[iIdx] = true
		members := make([]DuplicateCandidate, 0, len(cluster))
		for _, idx := range cluster {
			members = append(members, entries[idx].cand)
		}
		out = append(out, Cluster{
			Check:     "duplicates",
			MatchType: "title-fuzzy",
			Score:     roundTo(bestScore, 3),
			Members:   toMembers(members),
		})
	}

	return out
}

// roundTo truncates a float to n decimal places for display stability.
func roundTo(f float64, n int) float64 {
	shift := 1.0
	for i := 0; i < n; i++ {
		shift *= 10
	}
	return float64(int(f*shift+0.5)) / shift
}

// toMembers converts a slice of candidates into the display-ready member
// form. Kept as a helper so title and fuzzy clusterers can reuse it.
func toMembers(cands []DuplicateCandidate) []ClusterMember {
	out := make([]ClusterMember, len(cands))
	for i, c := range cands {
		out[i] = ClusterMember{
			Key:      c.Key,
			Title:    c.Title,
			Date:     c.Date,
			DOI:      c.DOI,
			PDFCount: c.PDFCount,
		}
	}
	return out
}
