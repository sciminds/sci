package match

import (
	"strings"

	"github.com/samber/lo"
)

// Token is one element of a tokenized search query.
//
// Text is the token's content (quotes stripped for quoted tokens, internal
// whitespace preserved for phrases). Quoted is true when the original form
// was `"..."` — callers use this to distinguish user-intended exact matches
// (quoted single word, quoted phrase) from the prefix-friendly default.
type Token struct {
	Text   string
	Quoted bool
}

// IsPhrase reports whether the token is a multi-word quoted phrase
// (Text contains whitespace). Quoted single words return false.
func (t Token) IsPhrase() bool {
	return t.Quoted && strings.ContainsAny(t.Text, " \t")
}

// TokenTexts extracts the Text field from each Token — useful when a caller
// only needs substring values (e.g. [MatchRow] or [TokenSpansInText]) and
// doesn't care about the Quoted flag.
func TokenTexts(tokens []Token) []string {
	return lo.Map(tokens, func(t Token, _ int) string { return t.Text })
}

// Tokenize splits a query string into Tokens.
//
// Unquoted runs split on whitespace; `"..."` runs stay as a single Token with
// internal whitespace preserved and Quoted=true. An empty quoted run (`""`)
// is skipped. An unterminated quote is lenient: the opening quote is dropped
// and the remainder is whitespace-split, so half-typed queries
// (`"alice bob` as the user types) still match incrementally.
//
// The returned tokens are in left-to-right query order. Callers that need
// plain strings can use [TokenTexts].
func Tokenize(query string) []Token {
	var out []Token
	i := 0
	n := len(query)
	for i < n {
		// Skip leading whitespace.
		for i < n && isSpace(query[i]) {
			i++
		}
		if i >= n {
			break
		}
		if query[i] == '"' {
			// Find matching close quote.
			end := strings.IndexByte(query[i+1:], '"')
			if end < 0 {
				// Unterminated — drop the quote and keep parsing the
				// remainder as whitespace-split tokens.
				i++
				continue
			}
			phraseStart := i + 1
			phraseEnd := phraseStart + end
			phrase := query[phraseStart:phraseEnd]
			if strings.TrimSpace(phrase) != "" {
				out = append(out, Token{Text: phrase, Quoted: true})
			}
			i = phraseEnd + 1
			continue
		}
		// Unquoted run: consume until whitespace or an opening quote.
		start := i
		for i < n && !isSpace(query[i]) && query[i] != '"' {
			i++
		}
		if i > start {
			out = append(out, Token{Text: query[start:i]})
		}
	}
	return out
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}
