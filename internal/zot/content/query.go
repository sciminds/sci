package content

import (
	"strings"
	"unicode"

	"github.com/samber/lo"
)

// MatchExpr translates a user's search string into an FTS5 MATCH
// expression. It returns "" when the query contains nothing searchable,
// which callers must treat as an error rather than passing along —
// FTS5 rejects an empty MATCH.
//
// The translation is deliberately conservative: every term is emitted as
// a quoted FTS5 string literal, so the user's input can never be parsed
// as FTS5 syntax. A query of `AND OR NOT` searches for those three words
// instead of producing a syntax error, and a stray `"` can't unbalance
// the expression. Two constructs survive translation because users
// expect them:
//
//   - "quoted phrases" stay phrases — the words must appear adjacent
//   - a trailing * makes the term a prefix query (neuro* → neuroimaging)
//
// Bare terms are ANDed, matching the behavior of the metadata search.
func MatchExpr(query string) string {
	terms := lo.FilterMap(tokenize(query), func(t token, _ int) (string, bool) {
		if !hasAlnum(t.text) {
			return "", false
		}
		// Doubling is FTS5's own escape for a quote inside a string literal.
		quoted := `"` + strings.ReplaceAll(t.text, `"`, `""`) + `"`
		if t.prefix {
			quoted += "*"
		}
		return quoted, true
	})
	return strings.Join(terms, " AND ")
}

// token is one unit of a parsed query: either a bare word or the
// contents of a quoted phrase.
type token struct {
	text   string
	prefix bool // the word carried a trailing *
}

// tokenize splits a query on whitespace, keeping "quoted phrases"
// together as single tokens. An unterminated quote runs to end of input
// rather than erroring — the user is mid-typing, not writing a program.
func tokenize(query string) []token {
	var out []token
	runes := []rune(query)
	for i := 0; i < len(runes); {
		switch {
		case unicode.IsSpace(runes[i]):
			i++
		case runes[i] == '"':
			i++ // consume the opening quote
			start := i
			for i < len(runes) && runes[i] != '"' {
				i++
			}
			out = append(out, token{text: string(runes[start:i])})
			if i < len(runes) {
				i++ // consume the closing quote
			}
		default:
			start := i
			for i < len(runes) && !unicode.IsSpace(runes[i]) {
				i++
			}
			word := string(runes[start:i])
			t := token{text: word}
			if strings.HasSuffix(word, "*") {
				t.text, t.prefix = strings.TrimSuffix(word, "*"), true
			}
			out = append(out, t)
		}
	}
	return out
}

// hasAlnum reports whether s contains anything the tokenizer would index.
// A term of pure punctuation ("-", ",") produces an empty FTS5 phrase,
// which matches nothing and muddies the expression, so it is dropped.
func hasAlnum(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	})
}
