package markdb

import (
	"strings"
	"testing"
)

func searchTestStore(t *testing.T) *Store {
	t.Helper()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "alpha.md", "---\ntitle: Alpha Post\ntags: [golang, sqlite]\n---\nThis is about databases and performance.")
	writeFile(t, dir, "beta.md", "---\ntitle: Beta Guide\n---\nA guide to testing and deployment strategies.")
	writeFile(t, dir, "gamma.md", "---\ntitle: Gamma\n---\nUnrelated content about cooking recipes.")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSearchBodyMatch(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("databases", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Path != "alpha.md" {
		t.Errorf("path = %q, want alpha.md", hits[0].Path)
	}
}

func TestSearchPhraseMatch(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search(`"deployment strategies"`, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Path != "beta.md" {
		t.Errorf("path = %q, want beta.md", hits[0].Path)
	}
}

func TestSearchNoResults(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("xyznonexistent", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits, want 0", len(hits))
	}
}

func TestSearchSnippet(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("cooking", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Snippet == "" {
		t.Error("snippet is empty")
	}
}

func TestSearchFrontmatter(t *testing.T) {
	s := searchTestStore(t)
	// "golang" appears only in frontmatter tags.
	hits, err := s.Search("golang", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Path != "alpha.md" {
		t.Errorf("path = %q, want alpha.md", hits[0].Path)
	}
}

func TestSearchRanked(t *testing.T) {
	s := searchTestStore(t)
	// "guide" appears in beta's title and body.
	hits, err := s.Search("guide", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("got 0 hits")
	}
	// Just verify we get results with rank values.
	for _, h := range hits {
		if h.Rank == 0 {
			t.Errorf("rank = 0 for %q, want non-zero", h.Path)
		}
	}
}

func TestSearchFTSSpecialChars(t *testing.T) {
	s := searchTestStore(t)

	// These FTS5 special-character queries should either return results or
	// return an error — but must never panic.
	edgeCases := []struct {
		name  string
		query string
	}{
		{"unbalanced quote", `"databases`},
		{"bare AND operator", `AND`},
		{"bare OR operator", `OR`},
		{"bare NOT operator", `NOT`},
		{"asterisk wildcard", `data*`},
		{"parenthesized group", `(databases OR cooking)`},
		{"unbalanced parens", `(databases`},
		{"empty string", ``},
		{"only whitespace", `   `},
		{"special punctuation", `what's up?`},
	}
	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			// We don't care whether it returns hits or errors — just that
			// it doesn't panic. If it errors, the error should be non-nil.
			hits, err := s.Search(tt.query, 50)
			if err != nil {
				// FTS5 parse error is acceptable for malformed queries.
				return
			}
			// If no error, hits should be a valid (possibly empty) slice.
			_ = hits
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := searchTestStore(t)
	_, err := s.Search("", 50)
	// Empty MATCH is a parse error in FTS5.
	if err == nil {
		t.Log("empty query returned no error (FTS5 accepted it)")
		return
	}
	// Error is expected and acceptable.
	if !strings.Contains(err.Error(), "fts5") && !strings.Contains(err.Error(), "syntax") {
		t.Logf("unexpected error type: %v", err)
	}
}

func TestSearchLimitZero(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("databases", 0)
	if err != nil {
		t.Fatal(err)
	}
	// limit <= 0 defaults to 50 internally, so should still find results.
	if len(hits) == 0 {
		t.Error("got 0 hits with limit=0, expected default limit to apply")
	}
}

func TestSearchNegativeLimit(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("databases", -5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Error("got 0 hits with negative limit, expected default to apply")
	}
}
