package content

import "testing"

func TestMatchExpr(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"single word", "gossip", `"gossip"`},
		{"two words AND", "prediction error", `"prediction" AND "error"`},
		{"quoted phrase stays a phrase", `"prediction error"`, `"prediction error"`},
		{"phrase plus word", `"prediction error" cortex`, `"prediction error" AND "cortex"`},
		{"trailing star is a prefix query", "neuro*", `"neuro"*`},
		{"embedded quote is doubled", `say"hi`, `"say""hi"`},
		{"fts5 operators are literals, not syntax", "AND OR NOT", `"AND" AND "OR" AND "NOT"`},
		{"punctuation-only token is dropped", "gossip - ,", `"gossip"`},
		{"case is preserved (tokenizer folds it)", "Gossip", `"Gossip"`},
		{"collapses whitespace", "  gossip   reputation  ", `"gossip" AND "reputation"`},
		{"unterminated quote is closed", `"prediction error`, `"prediction error"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchExpr(tt.query)
			if got != tt.want {
				t.Errorf("MatchExpr(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

// A query with nothing searchable in it must yield "", so callers can
// distinguish "no results" from "you gave me no terms" instead of
// handing FTS5 a syntactically invalid empty MATCH.
func TestMatchExprEmpty(t *testing.T) {
	for _, q := range []string{"", "   ", "-", `""`, ", . ;"} {
		if got := MatchExpr(q); got != "" {
			t.Errorf("MatchExpr(%q) = %q, want empty", q, got)
		}
	}
}
