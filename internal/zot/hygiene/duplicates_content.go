package hygiene

// duplicates_content.go — clustering by PDF bytes.
//
// The DOI and title passes answer "do these records describe the same
// work?" from metadata the user typed or an importer guessed. Neither
// can see that two items hold the same file: the live library's three
// copies of Bartlett's Remembering carry no DOI and two materially
// different titles ("A Study in experimental and Social Psychology" vs
// "An Experimental and Social Study"), so both passes miss them while
// each copy costs ~50 minutes of OCR. Byte identity is the signal that
// catches those.

import (
	"maps"
	"slices"

	"github.com/samber/lo"
)

// ClusterByContent groups candidates whose PDFs share a content key —
// [extract.ContentKey], the mtime-free projection of the extraction
// fingerprint, so separate downloads of the same file still match.
// contentKeys maps item key → content key; a candidate absent from it
// (no PDF) is not a candidate for this pass.
//
// An empty content key is never grouped. It means the hash failed or
// the file is gone, which is not evidence of sameness — grouping on it
// would collapse every unreadable PDF in the library into one enormous
// false positive.
func ClusterByContent(cands []DuplicateCandidate, contentKeys map[string]string) []Cluster {
	buckets := map[string][]DuplicateCandidate{}
	for _, c := range cands {
		key := contentKeys[c.Key]
		if key == "" {
			continue
		}
		buckets[key] = append(buckets[key], c)
	}

	// Sorted bucket keys keep the output stable across runs — the map
	// iteration order would otherwise reshuffle clusters between
	// invocations of a check people diff.
	keys := slices.Sorted(maps.Keys(buckets))

	return lo.FilterMap(keys, func(k string, _ int) (Cluster, bool) {
		members := buckets[k]
		if len(members) < 2 {
			return Cluster{}, false
		}
		return Cluster{
			Check:     "duplicates",
			MatchType: "content",
			Score:     1.0,
			Members:   toMembers(members),
		}, true
	})
}
