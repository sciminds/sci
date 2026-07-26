// Package bib builds bibliographies from documents: it scans markdown /
// Quarto text for citation references — pandoc @citekeys, [[wikilinks]],
// DOIs, arXiv ids, and URLs — and resolves each against the local Zotero
// library. Resolution never guesses: a reference matching multiple items
// is reported as unresolved (with the candidate count) rather than
// silently picking one, and unresolved references always surface in the
// result so a generated .bib can't quietly omit citations.
//
// The package is pure — no I/O. Callers read files, feed text to
// [ScanText], resolve with [Resolve], and hand the resolved items to the
// shared export pipeline (zot.ExportLibrary).
package bib

import (
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// RefKind classifies how a citation reference was written in the source
// document.
type RefKind string

// The reference kinds ScanText emits.
const (
	// KindCitekey is a pandoc-style @citekey (with or without brackets).
	KindCitekey RefKind = "citekey"
	// KindWikilink is an Obsidian-style [[target]] link (embeds excluded).
	KindWikilink RefKind = "wikilink"
	// KindDOI is a bare DOI or a doi.org URL, normalized to the DOI itself.
	KindDOI RefKind = "doi"
	// KindArxiv is an arXiv id from an arXiv: prefix or an arxiv.org URL,
	// normalized to the bare id (version suffix stripped).
	KindArxiv RefKind = "arxiv"
	// KindURL is any other http(s) URL.
	KindURL RefKind = "url"
)

// Ref is one citation reference found in a document.
type Ref struct {
	// Raw is the reference as it appeared in the text (for error messages).
	Raw string `json:"raw"`
	// Kind classifies the reference form.
	Kind RefKind `json:"kind"`
	// Value is the normalized payload: the citekey, wikilink target, DOI,
	// arXiv id, or URL.
	Value string `json:"value"`
}

var (
	wikilinkRe = regexp.MustCompile(`(!?)\[\[([^\[\]]+)\]\]`)
	urlRe      = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)
	doiRe      = regexp.MustCompile(`\b10\.\d{4,9}/[^\s"'<>\[\]]+`)
	// arxivTextRe matches the textual arXiv:NNNN.NNNNN form; ids inside
	// arxiv.org URLs are extracted by arxivURLRe during the URL pass.
	arxivTextRe = regexp.MustCompile(`(?i)\barxiv:\s*(\d{4}\.\d{4,5})`)
	arxivURLRe  = regexp.MustCompile(`(?i)arxiv\.org/(?:abs|pdf)/(\d{4}\.\d{4,5})`)
	doiURLRe    = regexp.MustCompile(`(?i)doi\.org/(10\.\d{4,9}/[^\s"'<>\)\]]+)`)
	// citekeyRe needs the leading-context group because Go regexps have no
	// lookbehind: a citekey's @ must open the text or follow whitespace,
	// [, ;, (, or - (pandoc's author-suppression prefix) — this is what
	// keeps email addresses from parsing as citekeys.
	citekeyRe = regexp.MustCompile(`(^|[\s\[;(-])@([A-Za-z][A-Za-z0-9_:.+/-]*[A-Za-z0-9]|[A-Za-z])`)
)

// trailingPunct is stripped from URL and DOI matches — sentence
// punctuation that the greedy character classes swallow.
const trailingPunct = ".,;:!?"

// span is a half-open matched region [start, end) used to keep later
// passes from re-matching text a higher-precedence pass already claimed.
type span struct{ start, end int }

func overlaps(spans []span, start, end int) bool {
	return lo.SomeBy(spans, func(s span) bool {
		return start < s.end && end > s.start
	})
}

// positioned pairs a Ref with its byte offset so emission order follows
// document order regardless of which scan pass found it.
type positioned struct {
	ref Ref
	pos int
}

// ScanText extracts every citation reference from a document. Passes run
// in precedence order (wikilinks, URLs, bare DOIs, arXiv ids, citekeys) so
// nested forms — a DOI inside a doi.org URL, a citekey inside a wikilink —
// are claimed once by the outermost form. Embeds (![[...]]) are skipped.
// Results are in document order, deduplicated by kind + case-insensitive
// value (first appearance wins).
func ScanText(text string) []Ref {
	var claimed []span
	var found []positioned
	add := func(pos int, r Ref) { found = append(found, positioned{ref: r, pos: pos}) }

	for _, m := range wikilinkRe.FindAllStringSubmatchIndex(text, -1) {
		claimed = append(claimed, span{m[0], m[1]})
		if text[m[2]:m[3]] == "!" {
			continue // embed, not a citation
		}
		target := text[m[4]:m[5]]
		// [[target|alias]] and [[target#heading]] both cite `target`.
		target, _, _ = strings.Cut(target, "|")
		target, _, _ = strings.Cut(target, "#")
		if target = strings.TrimSpace(target); target != "" {
			add(m[0], Ref{Raw: text[m[0]:m[1]], Kind: KindWikilink, Value: target})
		}
	}

	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		if overlaps(claimed, m[0], m[1]) {
			continue
		}
		claimed = append(claimed, span{m[0], m[1]})
		raw := strings.TrimRight(text[m[0]:m[1]], trailingPunct)
		switch {
		case doiURLRe.MatchString(raw):
			doi := doiURLRe.FindStringSubmatch(raw)[1]
			add(m[0], Ref{Raw: raw, Kind: KindDOI, Value: doi})
		case arxivURLRe.MatchString(raw):
			id := arxivURLRe.FindStringSubmatch(raw)[1]
			add(m[0], Ref{Raw: raw, Kind: KindArxiv, Value: id})
		default:
			add(m[0], Ref{Raw: raw, Kind: KindURL, Value: raw})
		}
	}

	for _, m := range doiRe.FindAllStringIndex(text, -1) {
		if overlaps(claimed, m[0], m[1]) {
			continue
		}
		claimed = append(claimed, span{m[0], m[1]})
		doi := strings.TrimRight(text[m[0]:m[1]], trailingPunct)
		add(m[0], Ref{Raw: doi, Kind: KindDOI, Value: doi})
	}

	for _, m := range arxivTextRe.FindAllStringSubmatchIndex(text, -1) {
		if overlaps(claimed, m[0], m[1]) {
			continue
		}
		claimed = append(claimed, span{m[0], m[1]})
		add(m[0], Ref{Raw: text[m[0]:m[1]], Kind: KindArxiv, Value: text[m[2]:m[3]]})
	}

	for _, m := range citekeyRe.FindAllStringSubmatchIndex(text, -1) {
		// m[4]:m[5] is the key; the @ sits one byte before it.
		if overlaps(claimed, m[4]-1, m[5]) {
			continue
		}
		add(m[4]-1, Ref{Raw: "@" + text[m[4]:m[5]], Kind: KindCitekey, Value: text[m[4]:m[5]]})
	}

	slices.SortStableFunc(found, func(a, b positioned) int { return a.pos - b.pos })
	deduped := lo.UniqBy(found, func(p positioned) string {
		return string(p.ref.Kind) + "\x00" + strings.ToLower(p.ref.Value)
	})
	return lo.Map(deduped, func(p positioned, _ int) Ref { return p.ref })
}
