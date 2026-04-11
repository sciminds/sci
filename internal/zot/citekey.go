package zot

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/sciminds/cli/internal/zot/local"
)

// englishStopwords are skipped when picking the first meaningful word of a
// title for synthesized cite-keys. Conservative list: articles, short
// prepositions, basic conjunctions. No pronouns, no verbs. Edit with care —
// every removal changes keys for existing libraries (drift).
var englishStopwords = map[string]bool{
	"a": true, "an": true, "the": true,
	"of": true, "on": true, "in": true, "for": true, "to": true,
	"with": true, "from": true, "by": true, "at": true, "as": true,
	"and": true, "or": true,
}

// synthesizeCiteKey builds a deterministic BibTeX cite-key from item metadata:
//
//	{lastname}{year}{firsttitleword}-{zoteroKey}
//
// Any of the three semantic tokens may be empty (no author → "anon", no year →
// omitted, no title → omitted). The 8-char Zotero key suffix guarantees
// uniqueness without per-key collision arithmetic and gives us a stable handle
// for round-tripping back to the original item.
func synthesizeCiteKey(it *local.Item) string {
	author := firstCreatorToken(it.Creators)
	if author == "" {
		author = "anon"
	}
	year := ""
	if y := yearFromDate(it.Date); y > 0 {
		year = fmt.Sprintf("%04d", y)
	}
	word := firstTitleWord(it.Title)
	return author + year + word + "-" + it.Key
}

// firstCreatorToken picks the lowest-OrderIdx author (falling back to editor
// if no authors exist) and returns its normalized last-name token.
func firstCreatorToken(cs []local.Creator) string {
	pickLowest := func(accept func(local.Creator) bool) *local.Creator {
		var best *local.Creator
		for i := range cs {
			if !accept(cs[i]) {
				continue
			}
			if best == nil || cs[i].OrderIdx < best.OrderIdx {
				best = &cs[i]
			}
		}
		return best
	}
	c := pickLowest(func(c local.Creator) bool { return c.Type == "author" || c.Type == "" })
	if c == nil {
		c = pickLowest(func(c local.Creator) bool { return c.Type == "editor" })
	}
	if c == nil {
		return ""
	}
	name := c.Last
	if name == "" {
		name = c.Name // institutional author (fieldMode=1)
	}
	return normalizeToken(name)
}

// firstTitleWord returns the first non-stopword token of a title. If every
// word is a stopword (e.g. "The The"), returns the first raw token so the
// caller still gets something typeable.
func firstTitleWord(title string) string {
	if title == "" {
		return ""
	}
	var firstRaw string
	for _, w := range strings.Fields(title) {
		tok := normalizeToken(w)
		if tok == "" {
			continue
		}
		if firstRaw == "" {
			firstRaw = tok
		}
		if !englishStopwords[tok] {
			return tok
		}
	}
	return firstRaw
}

// normalizeToken ASCII-folds via NFD + Mn-strip, lowercases, and drops any
// character that is not [a-z0-9]. Apostrophes, hyphens, punctuation, and
// diacritics all disappear. Output is safe to use as a cite-key fragment.
func normalizeToken(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	folded, _, _ := transform.String(t, s)
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ResolveCiteKey returns the cite-key for an item, honoring user-pinned keys
// before falling back to synthesis. Resolution order:
//
//  1. Native Zotero 7 `citationKey` field
//  2. Legacy BBT `Citation Key:` line in the `extra` field
//  3. Synthesized via synthesizeCiteKey
//
// The boolean return is true when we synthesized the key — exported entries
// flagged this way are subject to drift and should be tracked via the
// .zotero-citekeymap.json sidecar.
func ResolveCiteKey(it *local.Item) (string, bool) {
	if k := strings.TrimSpace(it.Fields["citationKey"]); k != "" {
		return k, false
	}
	if k := bbtKeyFromExtra(it.Fields["extra"]); k != "" {
		return k, false
	}
	return synthesizeCiteKey(it), true
}

var bbtExtraRe = regexp.MustCompile(`(?m)^Citation Key:\s*(\S+)\s*$`)

func bbtKeyFromExtra(extra string) string {
	m := bbtExtraRe.FindStringSubmatch(extra)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractExtraNote returns the user-prose portion of the `extra` field with
// structured BBT-style lines stripped. Used to preserve any existing user
// note content when emitting BibTeX `note` — we must not clobber it when
// appending the zotero:// round-trip URI.
func extractExtraNote(extra string) string {
	if extra == "" {
		return ""
	}
	lines := strings.Split(extra, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if strings.HasPrefix(ln, "Citation Key:") {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
