// Package doi implements publisher-specific DOI normalization. The only
// public surface is StripSubobject and its derivative IsSubobject — both
// trim publisher-known subobject suffixes (table/figure/supplement deep
// links, article-section anchors) so the parent-paper DOI can be resolved
// against OpenAlex.
//
// Patterns are anchored to the publisher prefix so non-target DOIs are
// never touched: a "10.1234/foo.t001" looks like a PLOS subobject but
// lives under an unrelated registrant, so it passes through unchanged.
//
// URL-prefix stripping (https://doi.org/, doi:) is intentionally out of
// scope here. Several near-identical helpers already exist elsewhere in
// the codebase (hygiene/duplicates.go, cli/read.go, enrich/mapping.go);
// consolidating those is a separate refactor.
package doi

import "regexp"

// frontiersSuffix matches Frontiers article-section deep links. Frontiers
// stores /abstract and /full on the URL, but Crossref carries the bare
// parent DOI; the suffixed form 404s on OpenAlex.
var frontiersSuffix = regexp.MustCompile(`^(10\.3389/.+?)/(?:abstract|full)$`)

// plosSubobject matches PLOS table (.t), figure (.g), and supplement (.s)
// subobjects. Numbers are bounded to avoid eating accidental DOIs that
// happen to end in a similar pattern.
var plosSubobject = regexp.MustCompile(`^(10\.1371/.+?)\.[tgs]\d{1,4}$`)

// pnasSupplemental matches PNAS supplemental-material DOIs. The form is
// either "/-/DCSupplemental" alone or with an additional path component
// (e.g. ".../pnas.201005062SI.pdf").
var pnasSupplemental = regexp.MustCompile(`^(10\.1073/.+?)/-/DCSupplemental(?:/.*)?$`)

// StripSubobject trims a publisher-specific subobject suffix from raw,
// returning the parent-paper DOI. Inputs that don't match any known
// pattern are returned unchanged.
func StripSubobject(raw string) string {
	if raw == "" {
		return raw
	}
	for _, re := range []*regexp.Regexp{frontiersSuffix, plosSubobject, pnasSupplemental} {
		if m := re.FindStringSubmatch(raw); m != nil {
			return m[1]
		}
	}
	return raw
}

// IsSubobject reports whether raw matches a known subobject pattern.
func IsSubobject(raw string) bool {
	return StripSubobject(raw) != raw
}
