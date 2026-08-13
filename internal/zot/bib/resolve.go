package bib

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/samber/lo"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/sciminds/sci/pkg/citekey"
	"github.com/sciminds/sci/pkg/local"
)

// Unresolved is a reference that could not be matched to exactly one
// library item, with the reason (no match, or an ambiguity the resolver
// refused to guess through).
type Unresolved struct {
	Ref
	// Reason explains the failure: "no match" or "ambiguous (N candidates)".
	Reason string `json:"reason"`
	// Candidates names the competing Zotero item keys when the reference was
	// ambiguous, sorted for stable output. Empty for a no-match — an empty
	// list would read as "we found something", which inverts the honesty
	// contract. Callers turn these into a disambiguation fix.
	Candidates []string `json:"candidates,omitempty"`
}

// zotKeySuffixRe extracts the trailing 8-char Zotero key from a
// synthesized cite-key ({author}{year}-{words}-{ZOTKEY}) so keys from an
// earlier export resolve even after their prefix drifted.
var zotKeySuffixRe = regexp.MustCompile(`-([A-Z0-9]{8})$`)

// libraryIndex holds the lookup tables Resolve builds once per call.
type libraryIndex struct {
	items        []*local.Item
	byCitekey    map[string][]*local.Item // lower(resolved cite-key)
	byZotKey     map[string]*local.Item   // 8-char Zotero key (unique)
	byDOI        map[string][]*local.Item // normalized DOI
	byTitle      map[string][]*local.Item // normalized title
	byAuthorYear map[string][]*local.Item // normalized "{lastname}{year}"
}

// RefMatch pairs one reference with the library item it resolved to.
type RefMatch struct {
	Ref  Ref        `json:"ref"`
	Item local.Item `json:"item"`
}

// ResolveRefs matches each reference against the library, keeping the
// per-reference mapping: one RefMatch per reference that resolved, in
// document order, plus every reference that didn't. Not deduplicated — the
// same paper cited twice yields two matches.
//
// [Resolve] is the deduplicated view of this same walk and is what a
// bibliography wants. Callers that need to know WHICH reference produced an
// item — to report that a paper was cited both as a zotero:// link and as a
// DOI — use this instead.
func ResolveRefs(refs []Ref, items []local.Item) ([]RefMatch, []Unresolved) {
	idx := buildIndex(items)

	var matches []RefMatch
	var unresolved []Unresolved
	for _, ref := range refs {
		it, candidates := idx.lookup(ref)
		if it == nil {
			reason := "no match"
			if len(candidates) > 1 {
				reason = fmt.Sprintf("ambiguous (%d candidates)", len(candidates))
			}
			unresolved = append(unresolved, Unresolved{Ref: ref, Reason: reason, Candidates: candidates})
			continue
		}
		matches = append(matches, RefMatch{Ref: ref, Item: *it})
	}
	return matches, unresolved
}

// Resolve matches each reference against the library and returns the
// resolved items (deduplicated by Zotero key, in first-appearance order)
// plus every reference that didn't resolve. A reference matching more
// than one distinct item is unresolved — the resolver never guesses.
//
// Cite-keys resolve through the same policy as export ([citekey.Resolve]:
// native Zotero 7 field, BBT extra line, synthesized fallback), so a key
// produced by `zot export` always round-trips.
func Resolve(refs []Ref, items []local.Item) ([]local.Item, []Unresolved) {
	matches, unresolved := ResolveRefs(refs, items)
	deduped := lo.UniqBy(matches, func(m RefMatch) string { return m.Item.Key })
	if len(deduped) == 0 {
		// A nil slice, not an empty one: callers (and their golden JSON)
		// have always seen `null` here for a document that cites nothing.
		return nil, unresolved
	}
	return lo.Map(deduped, func(m RefMatch, _ int) local.Item { return m.Item }), unresolved
}

func buildIndex(items []local.Item) *libraryIndex {
	idx := &libraryIndex{
		byCitekey:    map[string][]*local.Item{},
		byZotKey:     map[string]*local.Item{},
		byDOI:        map[string][]*local.Item{},
		byTitle:      map[string][]*local.Item{},
		byAuthorYear: map[string][]*local.Item{},
	}
	for i := range items {
		it := &items[i]
		idx.items = append(idx.items, it)
		key, _ := citekey.Resolve(it)
		idx.byCitekey[strings.ToLower(key)] = append(idx.byCitekey[strings.ToLower(key)], it)
		idx.byZotKey[it.Key] = it
		if it.DOI != "" {
			d := normalizeDOI(it.DOI)
			idx.byDOI[d] = append(idx.byDOI[d], it)
		}
		if t := normalize(it.Title); t != "" {
			idx.byTitle[t] = append(idx.byTitle[t], it)
		}
		if ay := authorYearKey(it); ay != "" {
			idx.byAuthorYear[ay] = append(idx.byAuthorYear[ay], it)
		}
	}
	return idx
}

// lookup resolves one reference. Returns the single match, or nil plus the
// distinct candidate keys (empty = no match, >1 = ambiguous).
func (idx *libraryIndex) lookup(ref Ref) (*local.Item, []string) {
	switch ref.Kind {
	case KindCitekey:
		return idx.lookupCitekey(ref.Value)
	case KindWikilink:
		// A wikilink target may be a cite-key, a title, or "Author Year".
		if it, n := idx.lookupCitekey(ref.Value); it != nil {
			return it, n
		}
		if cands := idx.byTitle[normalize(ref.Value)]; len(cands) > 0 {
			return unique(cands)
		}
		return unique(idx.byAuthorYear[normalize(ref.Value)])
	case KindZoteroKey:
		// The key IS the item id, so there is nothing to guess — but it
		// still goes through the index rather than being trusted, which is
		// what turns a stale link in an old note into an honest
		// "no match" instead of a dangling relation.
		if it, ok := idx.byZotKey[ref.Value]; ok {
			return it, nil
		}
		return nil, nil
	case KindDOI:
		return unique(idx.byDOI[normalizeDOI(ref.Value)])
	case KindArxiv:
		return unique(lo.Filter(idx.items, func(it *local.Item, _ int) bool {
			if strings.Contains(it.URL, ref.Value) {
				return true
			}
			// DataCite registers arXiv preprints as 10.48550/arXiv.<id>.
			return normalizeDOI(it.DOI) == "10.48550/arxiv."+ref.Value
		}))
	case KindURL:
		if it, n := unique(lo.Filter(idx.items, func(it *local.Item, _ int) bool {
			return it.URL == ref.Value
		})); it != nil {
			return it, n
		}
		// Fall back to substring in either direction — catches trailing
		// slashes, protocol variants, and shortened forms.
		return unique(lo.Filter(idx.items, func(it *local.Item, _ int) bool {
			return it.URL != "" &&
				(strings.Contains(it.URL, ref.Value) || strings.Contains(ref.Value, it.URL))
		}))
	default:
		return nil, nil
	}
}

func (idx *libraryIndex) lookupCitekey(value string) (*local.Item, []string) {
	if cands := idx.byCitekey[strings.ToLower(value)]; len(cands) > 0 {
		return unique(cands)
	}
	// A synthesized key whose prefix drifted still carries the Zotero key.
	if m := zotKeySuffixRe.FindStringSubmatch(strings.ToUpper(value)); m != nil {
		if it, ok := idx.byZotKey[m[1]]; ok {
			return it, nil
		}
	}
	return nil, nil
}

// unique returns the single distinct item in cands. When there isn't exactly
// one it returns nil plus the distinct Zotero keys, sorted — empty for no
// match, the competing keys for an ambiguity.
func unique(cands []*local.Item) (*local.Item, []string) {
	distinct := lo.UniqBy(cands, func(it *local.Item) string { return it.Key })
	switch len(distinct) {
	case 0:
		return nil, nil
	case 1:
		return distinct[0], nil
	}
	keys := lo.Map(distinct, func(it *local.Item, _ int) string { return it.Key })
	slices.Sort(keys)
	return nil, keys
}

// normalizeDOI lowercases a DOI and strips resolver-URL and "doi:"
// prefixes so stored and cited forms compare equal.
func normalizeDOI(doi string) string {
	d := strings.TrimSpace(strings.ToLower(doi))
	d = strings.TrimPrefix(d, "https://doi.org/")
	d = strings.TrimPrefix(d, "http://doi.org/")
	d = strings.TrimPrefix(d, "doi:")
	return d
}

// authorYearKey builds the normalized "{lastname}{year}" lookup key for
// an item, or "" when it lacks an author or a year.
func authorYearKey(it *local.Item) string {
	author := lo.MinBy(
		lo.Filter(it.Creators, func(c local.Creator, _ int) bool {
			return c.Type == "author" || c.Type == ""
		}),
		func(a, b local.Creator) bool { return a.OrderIdx < b.OrderIdx },
	)
	name := author.Last
	if name == "" {
		name = author.Name // institutional author (fieldMode=1)
	}
	if name == "" || it.Year == 0 {
		return ""
	}
	return normalize(fmt.Sprintf("%s %d", name, it.Year))
}

// normalize ASCII-folds (NFD + combining-mark strip), lowercases, and
// drops every non-alphanumeric rune — the same folding cite-key synthesis
// uses, applied to whole strings for title / author-year equality.
func normalize(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, _ := transform.String(t, s)
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range strings.ToLower(folded) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
