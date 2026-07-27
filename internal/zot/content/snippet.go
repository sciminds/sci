package content

import (
	"strings"
	"unicode"

	"github.com/samber/lo"
)

// echoTitleShare is the fraction of a snippet's words that must also appear
// in the title before the snippet counts as an echo. It is deliberately below
// 1.0: the real echoes drag along front-matter that never reaches the title
// (a DOI, a markdown "##", a stray bracket), and demanding a perfect subset
// would let every one of them through.
const echoTitleShare = 0.8

// EchoesTitle reports whether snippet tells the reader nothing the title
// already did.
//
// A snippet exists to answer "why did this paper match?". When the query hit
// on the title, FTS5's best excerpt is usually the title again — docling
// opens every extraction with the title as a heading, so the highest-scoring
// span in the body is a restatement of the line directly above it. On a live
// `zot search hyperscanning --content`, three of eight hits spent their
// snippet line proving nothing.
//
// Detection is share-based rather than substring-based because the echoes are
// rarely clean slices: they carry a DOI, a "##", or a bracket picked up from
// the extraction's front matter. Comparing word sets and allowing a small
// remainder catches those; a genuine body sentence shares only incidental
// words with the title and stays well under the bar.
//
// An empty snippet or an empty title returns false — there is either nothing
// to suppress or no evidence to suppress it on, and dropping a snippet we
// cannot judge would lose real information.
func EchoesTitle(snippet, title string) bool {
	snipWords := contentWords(snippet)
	titleWords := contentWords(title)
	if len(snipWords) == 0 || len(titleWords) == 0 {
		return false
	}

	inTitle := lo.SliceToMap(titleWords, func(w string) (string, struct{}) {
		return w, struct{}{}
	})
	shared := lo.CountBy(snipWords, func(w string) bool {
		_, ok := inTitle[w]
		return ok
	})
	return float64(shared)/float64(len(snipWords)) >= echoTitleShare
}

// contentWords lowercases s and splits it into comparable word tokens,
// discarding the punctuation that snippet marks, ellipses, and markdown
// headings drag in. Tokens keep interior '.' and '/' so a DOI survives as one
// token instead of shattering into meaningless fragments.
func contentWords(s string) []string {
	fields := strings.FieldsFunc(stripTags(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '/' && r != '-'
	})
	return lo.FilterMap(fields, func(f string, _ int) (string, bool) {
		trimmed := strings.Trim(f, "./-")
		return trimmed, trimmed != ""
	})
}

// stripTags removes <...> spans so caller-supplied match marks
// ([Query.SnippetOpen] / [Query.SnippetClose], typically <b>…</b>) don't
// contribute phantom tokens. Without it a marked-up echo scores lower than
// the same text unmarked, which would make suppression depend on how the
// caller chose to highlight.
func stripTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}
