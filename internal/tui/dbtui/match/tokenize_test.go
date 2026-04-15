package match

import (
	"reflect"
	"testing"
)

// Tokenize splits a query into Tokens. Unquoted runs split on whitespace;
// double-quoted runs stay together as one token (quotes stripped, internal
// whitespace preserved). Quoted tokens carry Quoted=true so downstream code
// can distinguish `"foo"` (explicit exact match) from `foo` (prefix-friendly)
// and phrase tokens (Text contains whitespace) from single words.

func tokenEq(a, b []Token) bool { return reflect.DeepEqual(a, b) }

func TestTokenize_SimpleSplit(t *testing.T) {
	got := Tokenize("alice bob")
	want := []Token{{Text: "alice"}, {Text: "bob"}}
	if !tokenEq(got, want) {
		t.Errorf("Tokenize = %+v, want %+v", got, want)
	}
}

func TestTokenize_QuotedPhraseKeptTogether(t *testing.T) {
	got := Tokenize(`gossip "gossip drives" deposition`)
	want := []Token{
		{Text: "gossip"},
		{Text: "gossip drives", Quoted: true},
		{Text: "deposition"},
	}
	if !tokenEq(got, want) {
		t.Errorf("Tokenize = %+v, want %+v", got, want)
	}
}

func TestTokenize_QuotedSingleWordFlagged(t *testing.T) {
	got := Tokenize(`"brain"`)
	want := []Token{{Text: "brain", Quoted: true}}
	if !tokenEq(got, want) {
		t.Errorf("Tokenize = %+v, want %+v", got, want)
	}
}

func TestTokenize_EmptyPhraseSkipped(t *testing.T) {
	got := Tokenize(`alice "" bob`)
	want := []Token{{Text: "alice"}, {Text: "bob"}}
	if !tokenEq(got, want) {
		t.Errorf("Tokenize = %+v, want %+v", got, want)
	}
}

func TestTokenize_UnclosedQuote_Lenient(t *testing.T) {
	// Unterminated quote: fall back to whitespace-splitting the remainder so
	// a half-typed query still matches something incrementally.
	got := Tokenize(`"alice bob`)
	want := []Token{{Text: "alice"}, {Text: "bob"}}
	if !tokenEq(got, want) {
		t.Errorf("Tokenize = %+v, want %+v", got, want)
	}
}

func TestTokenize_Empty(t *testing.T) {
	if got := Tokenize(""); len(got) != 0 {
		t.Errorf("Tokenize(\"\") = %+v, want empty", got)
	}
	if got := Tokenize("   "); len(got) != 0 {
		t.Errorf("Tokenize whitespace = %+v, want empty", got)
	}
}

func TestTokenTexts(t *testing.T) {
	got := TokenTexts(Tokenize(`alice "gossip drives"`))
	want := []string{"alice", "gossip drives"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TokenTexts = %v, want %v", got, want)
	}
}

func TestMatchRow_QuotedPhrase_MatchesContiguous(t *testing.T) {
	tokens := TokenTexts(Tokenize(`"gossip drives"`))
	cells := []string{"gossip drives deposition"}
	if _, ok := MatchRow(tokens, cells, -1); !ok {
		t.Error("expected phrase to match contiguous run")
	}
}

func TestMatchRow_QuotedPhrase_RejectsNonAdjacent(t *testing.T) {
	tokens := TokenTexts(Tokenize(`"gossip drives"`))
	cells := []string{"gossip about drives"}
	if _, ok := MatchRow(tokens, cells, -1); ok {
		t.Error("phrase must not match non-adjacent words")
	}
}

func TestMatchRow_Unquoted_StillANDsScatteredWords(t *testing.T) {
	tokens := TokenTexts(Tokenize("gossip drives"))
	cells := []string{"gossip about drives"}
	if _, ok := MatchRow(tokens, cells, -1); !ok {
		t.Error("unquoted multi-token should AND-match scattered words")
	}
}
