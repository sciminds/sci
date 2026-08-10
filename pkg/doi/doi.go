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
// Which prefixes appear in a pattern is a MEASUREMENT, not a guess. Each
// was counted across the ~61k DOIs in the item and reference planes of a
// real library before it was written, and every shape was confirmed at
// doi.org: the suffixed form is a "component", a "peer-review", or a 404,
// and the stripped form is the paper.
//
// URL-prefix stripping (https://doi.org/, doi:) is intentionally out of
// scope here. Several near-identical helpers already exist elsewhere in
// the codebase (hygiene/duplicates.go, cli/read.go, enrich/mapping.go);
// consolidating those is a separate refactor.
package doi

import "regexp"

// Every pattern is case-insensitive. DOIs are case-insensitive by spec,
// and Zotero stores whatever the publisher's page said — so the same
// supplement arrives as /-/DCSupplemental from one item and
// /-/dcsupplemental from another.

// sectionSuffix matches the /abstract and /full article-section deep
// links. This is a PLATFORM path segment rather than a publisher one:
// Frontiers writes it, and so does Wiley's onlinelibrary, which is why an
// earlier Frontiers-only version of this pattern walked past 15 of the 49
// occurrences in the reference corpus. Both forms 404 at doi.org while
// their parents resolve, so the suffixed DOI names nothing at all.
var sectionSuffix = regexp.MustCompile(`(?i)^(10\.(?:3389|1111|1002)/.+?)/(?:abstract|full)$`)

// plosSubobject matches PLOS table (.t), figure (.g), and supplement (.s)
// subobjects. Numbers are bounded to avoid eating accidental DOIs that
// happen to end in a similar pattern.
var plosSubobject = regexp.MustCompile(`(?i)^(10\.1371/.+?)\.[tgs]\d{1,4}$`)

// supplementalDC matches the "/-/DC" supplemental convention, which PNAS
// spells /-/DCSupplemental (optionally with a file path) and OUP spells
// /-/DC1. One pattern because it is one convention wearing two spellings.
var supplementalDC = regexp.MustCompile(`(?i)^(10\.(?:1073|1093)/.+?)/-/DC(?:Supplemental)?\d*(?:/.*)?$`)

// elifeAsset matches eLife's per-asset child DOIs. Every figure, table and
// peer-review artifact of an eLife paper gets its own .NNN suffix, and
// doi.org types them accordingly — 10.7554/eLife.17089.001 is a
// "component" titled "Abstract", 10.7554/elife.01867.014 a "peer-review"
// author response. Neither is the paper.
var elifeAsset = regexp.MustCompile(`(?i)^(10\.7554/elife\.\d+)\.\d{3}$`)

// peerjSubobject matches PeerJ's supplement, figure, and table children.
var peerjSubobject = regexp.MustCompile(`(?i)^(10\.7717/peerj(?:-cs)?\.\d+)/(?:supp|fig|table)-\d+$`)

// StripSubobject trims a publisher-specific subobject suffix from raw,
// returning the parent-paper DOI. Inputs that don't match any known
// pattern are returned unchanged.
func StripSubobject(raw string) string {
	if raw == "" {
		return raw
	}
	for _, re := range []*regexp.Regexp{
		sectionSuffix, plosSubobject, supplementalDC, elifeAsset, peerjSubobject,
	} {
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
