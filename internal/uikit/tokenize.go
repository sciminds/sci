package uikit

import "strings"

// TokenizeQuery splits a query into substring tokens, preserving quoted
// phrases as single tokens with internal whitespace kept. This is the
// shared tokenizer used by overlay /-search and markdown highlight so that
// `"gossip drives"` highlights only contiguous runs — same semantics as the
// row-level phrase search in dbtui.
//
// Unterminated quotes are lenient: the opening quote is dropped and the
// remainder is whitespace-split so half-typed queries still match
// incrementally.
//
// Mirrors match.Tokenize in the dbtui/match package; kept here to preserve
// layering (uikit has no dependency on dbtui internals).
func TokenizeQuery(query string) []string {
	var out []string
	i := 0
	n := len(query)
	for i < n {
		for i < n && isQuerySpace(query[i]) {
			i++
		}
		if i >= n {
			break
		}
		if query[i] == '"' {
			end := strings.IndexByte(query[i+1:], '"')
			if end < 0 {
				i++
				continue
			}
			phrase := query[i+1 : i+1+end]
			if strings.TrimSpace(phrase) != "" {
				out = append(out, phrase)
			}
			i = i + 1 + end + 1
			continue
		}
		start := i
		for i < n && !isQuerySpace(query[i]) && query[i] != '"' {
			i++
		}
		if i > start {
			out = append(out, query[start:i])
		}
	}
	return out
}

func isQuerySpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}
