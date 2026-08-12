package local

// Tests for the bare-prefix search syntax: `tag:cats` and `-tag:cats`
// work without the `@` sigil, normalized by NormalizeQuery into the
// @field:/Negate forms match.ParseClauses already understands. This makes
// sci the single DSL owner for downstream consumers (zen) whose queries
// use the bare form — semantics were always shared, only syntax differed.

import (
	"slices"
	"testing"
)

func TestNormalizeQuery(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		// Bare prefixes gain the @ sigil.
		"tag:cats":            "@tag: cats",
		"author:smith":        "@author: smith",
		"year:2023 type:book": "@year: 2023 @type: book",
		// Leading `-` moves onto the value, where applyNegate finds it.
		"-tag:cats":          "@tag: -cats",
		"cortex -tag:cats":   "cortex @tag: -cats",
		"-author:smith fMRI": "@author: -smith fMRI",
		// Detached values keep working: `tag:` alone scopes the next token.
		"tag: cats": "@tag: cats",
		// A detached `-field:` has nowhere to hang the negation — untouched.
		"-tag: cats": "-tag: cats",
		// Field names are case-insensitive, mirroring buildClauseSQL.
		"Tag:cats": "@Tag: cats",
		// Already-sigiled clauses and unknown prefixes pass through.
		"@tag: cats":      "@tag: cats",
		"re:thinking":     "re:thinking",
		"https://a.b/c":   "https://a.b/c",
		"plain free text": "plain free text",
		// Quoted spans are opaque — a colon inside a phrase is not a field.
		`"tag:cats"`:                 `"tag:cats"`,
		`"attention: review" cortex`: `"attention: review" cortex`,
		// A quote opened earlier shields later tokens.
		`"deep tag:cats learning"`: `"deep tag:cats learning"`,
		// OR groups normalize per token.
		"type:book | type:thesis": "@type: book | @type: thesis",
	}
	for in, want := range cases {
		if got := NormalizeQuery(in); got != want {
			t.Errorf("NormalizeQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearch_BareFieldPrefix(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	items, err := db.Search("tag:cats", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Key != "CCCC3333" {
		t.Errorf("tag:cats = %v, want [CCCC3333]", keysOf(items))
	}
}

func TestSearch_BareFieldNegation(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// `-tag:cats` excludes the cats book and keeps everything else —
	// the clause-leading negation zen's DSL uses.
	items, err := db.Search("-tag:cats", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	if slices.Contains(got, "CCCC3333") {
		t.Errorf("-tag:cats still matched CCCC3333: %v", got)
	}
	for _, want := range []string{"AAAA1111", "BBBB2222", "GGGG7777"} {
		if !slices.Contains(got, want) {
			t.Errorf("-tag:cats dropped %s: %v", want, got)
		}
	}
}

func TestSearch_BareNegationCombinesWithFreeText(t *testing.T) {
	t.Parallel()
	db := openFixture(t)
	// "neuro" alone matches AAAA1111 (title) AND BBBB2222 (publication
	// NeuroImage); the bare negation must carve AAAA1111 out while the
	// free text keeps doing its job — proving the mixed group really
	// parses as free-text AND NOT-tag, not as one literal string.
	items, err := db.Search("neuro -tag:deep-learning", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(items)
	if !slices.Contains(got, "BBBB2222") {
		t.Errorf("free text stopped matching BBBB2222: %v", got)
	}
	if slices.Contains(got, "AAAA1111") {
		t.Errorf("negated tag still matched AAAA1111: %v", got)
	}
}

func TestQueryFreeText_NormalizesBarePrefixes(t *testing.T) {
	t.Parallel()
	// The content index must not receive `tag:cats` as free text — the
	// same normalization SearchWithTotal applies has to reach the
	// snippets path, or a bare-prefix query would ask the index a
	// different question than the metadata search answered.
	if got := QueryFreeText("cortex -tag:cats"); got != "cortex" {
		t.Errorf("QueryFreeText = %q, want %q", got, "cortex")
	}
}
