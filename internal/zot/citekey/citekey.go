// Package citekey owns BibTeX cite-key policy for the zot tools: the v2
// synthesis format, the validator that scores stored keys against it, and
// the legacy BBT `Citation Key:` extractor. Lives in a sub-package so
// both `zot` (for library export) and `zot/hygiene` (for the citekeys
// check) can depend on it without an import cycle — package zot already
// imports zot/hygiene for the doctor dispatch, so cite-key policy cannot
// live up there alongside it.
package citekey

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/samber/lo"
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

// wordCount / wordMaxLen cap the title-words segment of a synthesized
// cite-key. Keep in sync with the `words` segment length in citeKeyV2Re
// (wordCount * wordMaxLen). Changing either constant rewrites every
// synthesized key in every library that upgrades — treat as a breaking
// change.
const (
	wordCount  = 3
	wordMaxLen = 4
)

// Synthesize builds a deterministic BibTeX cite-key from item metadata.
// Format (v2):
//
//	{author}{year}-{words}-{zoteroKey}
//
// Where `{author}` is the lowercase last name of the first author (or
// `anon`), `{year}` is the 4-digit publication year, `{words}` is up to
// the first three non-stopword title tokens each truncated to 4 characters
// and concatenated with no separator, and `{zoteroKey}` is the 8-char
// Zotero item key.
//
// The author and year segments are fused (no separator between them).
// Year and words are each independently optional: if year is missing the
// fusion collapses to just `{author}`, and if words are missing that
// segment — including its leading hyphen — is omitted entirely. The
// Zotero key suffix is always present and guarantees uniqueness without
// collision arithmetic.
func Synthesize(it *local.Item) string {
	author := firstCreatorToken(it.Creators)
	if author == "" {
		author = "anon"
	}
	year := ""
	if y := yearFromDate(it.Date); y > 0 {
		year = fmt.Sprintf("%04d", y)
	}
	words := firstThreeTitleWords(it.Title)

	parts := []string{author + year}
	if words != "" {
		parts = append(parts, words)
	}
	parts = append(parts, it.Key)
	return strings.Join(parts, "-")
}

// Resolve returns the cite-key for an item, honoring user-pinned keys
// before falling back to synthesis. Resolution order:
//
//  1. Native Zotero 7 `citationKey` field
//  2. Legacy BBT `Citation Key:` line in the `extra` field
//  3. Synthesized via Synthesize
//
// The boolean return is true when we synthesized the key — exported
// entries flagged this way are subject to drift and should be tracked
// via the .zotero-citekeymap.json sidecar.
func Resolve(it *local.Item) (string, bool) {
	if k := strings.TrimSpace(it.Fields["citationKey"]); k != "" {
		return k, false
	}
	if k := FromExtra(it.Fields["extra"]); k != "" {
		return k, false
	}
	return Synthesize(it), true
}

// Status is the validation verdict for a cite-key string. See Validate
// for the per-status semantics.
type Status int

const (
	// Valid means the key matches our v2 spec (canonical synthesized form:
	// {author}{year}-{words}-{ZOTKEY}).
	Valid Status = iota
	// NonCanonical means the key is BibTeX-legal but does not match our v2
	// spec — e.g. a hand-authored key, a BBT-managed key, or a drifted
	// synthesized key from an older spec version.
	NonCanonical
	// Invalid means the key is structurally broken: empty, contains
	// whitespace, or contains a BibTeX-illegal character.
	Invalid
)

// citeKeyV2Re enforces our synthesized spec. Pattern in plain English:
//
//	{author}          one or more lowercase letters
//	{year}            optional 4 digits, fused to author with no separator
//	{words?}          optional hyphen-separated lowercase alnum up to
//	                  wordCount*wordMaxLen chars
//	-{ZOTKEY}         hyphen + 8 uppercase alnum (Zotero key alphabet)
var citeKeyV2Re = regexp.MustCompile(`^[a-z]+(\d{4})?(-[a-z0-9]{1,12})?-[A-Z0-9]{8}$`)

// citeKeyBadRune reports characters that make a cite-key structurally
// invalid in BibTeX. Whitespace is handled separately via unicode.IsSpace
// so we catch tabs, newlines, and NBSP as well as plain spaces. Derived
// from the BibTeX parser rules: `,` and `=` break the entry header,
// `{`/`}` break scope balancing, `%` starts a comment, `#` concatenates
// in the value grammar, `~` is a non-breaking space, and `\` starts a
// command. `"` is included defensively because some downstream processors
// treat it as a delimiter.
func citeKeyBadRune(r rune) bool {
	switch r {
	case ',', '{', '}', '=', '%', '#', '~', '\\', '"':
		return true
	}
	return false
}

// Validate returns the status and a short human-readable reason for
// invalid keys. Reason is empty when status is Valid.
//
// The check runs in two stages. Stage one looks for structural breakage:
// an empty string, non-printable runes, whitespace, or any of the BibTeX
// metacharacters flagged by citeKeyBadRune. These return Invalid
// regardless of shape — no downstream BibTeX processor will survive them.
// Stage two matches the surviving candidates against our v2 spec regex;
// anything that passes is canonical, anything that doesn't is flagged as
// non-canonical (a soft warning, not an error — BBT-managed libraries
// produce 100% non-canonical keys by design and that's fine).
func Validate(key string) (Status, string) {
	if key == "" {
		return Invalid, "empty"
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return Invalid, "contains non-printable character"
		}
		if unicode.IsSpace(r) {
			return Invalid, "contains whitespace"
		}
		if citeKeyBadRune(r) {
			return Invalid, "contains BibTeX-illegal character"
		}
	}
	if citeKeyV2Re.MatchString(key) {
		return Valid, ""
	}
	return NonCanonical, "does not match {author}{year}-{words}-{ZOTKEY} spec"
}

var bbtExtraRe = regexp.MustCompile(`(?m)^Citation Key:\s*(\S+)\s*$`)

// FromExtra parses a legacy Better BibTeX `Citation Key: foo` line out of
// a Zotero `extra` field. Returns "" when no such line is present.
func FromExtra(extra string) string {
	m := bbtExtraRe.FindStringSubmatch(extra)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// ExtractNote returns the user-prose portion of the `extra` field with
// structured BBT-style lines stripped. Used by the BibTeX exporter to
// preserve any existing user note content when appending the zotero://
// round-trip URI — we must not clobber it.
func ExtractNote(extra string) string {
	if extra == "" {
		return ""
	}
	lines := strings.Split(extra, "\n")
	kept := lo.Filter(lines, func(ln string, _ int) bool {
		return !strings.HasPrefix(ln, "Citation Key:")
	})
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// firstCreatorToken picks the lowest-OrderIdx author (falling back to
// editor if no authors exist) and returns its normalized last-name token.
func firstCreatorToken(cs []local.Creator) string {
	cmpIdx := func(a, b local.Creator) bool { return a.OrderIdx < b.OrderIdx }

	candidates := lo.Filter(cs, func(c local.Creator, _ int) bool {
		return c.Type == "author" || c.Type == ""
	})
	if len(candidates) == 0 {
		candidates = lo.Filter(cs, func(c local.Creator, _ int) bool {
			return c.Type == "editor"
		})
	}
	if len(candidates) == 0 {
		return ""
	}
	c := lo.MinBy(candidates, cmpIdx)
	name := c.Last
	if name == "" {
		name = c.Name // institutional author (fieldMode=1)
	}
	return normalizeToken(name)
}

// firstThreeTitleWords picks up to wordCount leading non-stopword tokens
// from a title, truncates each to wordMaxLen runes, and concatenates them
// with no separator. If every token in the title is a stopword (e.g.
// "The The"), falls back to the first raw token — still truncated — so
// the caller always gets something typeable when any title text exists.
// Returns "" only when the title is empty or yields no normalizable
// tokens.
func firstThreeTitleWords(title string) string {
	if title == "" {
		return ""
	}
	var picks []string
	var firstRaw string
	for _, w := range strings.Fields(title) {
		tok := normalizeToken(w)
		if tok == "" {
			continue
		}
		if firstRaw == "" {
			firstRaw = tok
		}
		if englishStopwords[tok] {
			continue
		}
		picks = append(picks, truncate(tok, wordMaxLen))
		if len(picks) == wordCount {
			break
		}
	}
	if len(picks) == 0 {
		return truncate(firstRaw, wordMaxLen)
	}
	return strings.Join(picks, "")
}

// truncate shortens a lowercase ASCII token to at most n runes.
// normalizeToken guarantees single-byte ASCII so byte slicing is safe.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// normalizeToken ASCII-folds via NFD + Mn-strip, lowercases, and drops
// any character that is not [a-z0-9]. Apostrophes, hyphens, punctuation,
// and diacritics all disappear. Output is safe to use as a cite-key
// fragment.
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

// yearFromDate returns the four-digit year from a Zotero date field, or
// 0 if none is parseable. Handles both raw "YYYY" and Zotero's
// "YYYY-MM-DD originalText" dual-encoding. Kept here (rather than in the
// parent package's export.go) so the citekey package has everything
// Synthesize needs without a back-import.
func yearFromDate(date string) int {
	s := strings.TrimSpace(date)
	if s == "" {
		return 0
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	if len(s) < 4 {
		return 0
	}
	y := 0
	for i := 0; i < 4; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}
