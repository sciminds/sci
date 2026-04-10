package markdb

import (
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

	// Queries with FTS5 special characters must not return errors — the
	// Search function should sanitize input so users never see FTS parse
	// failures. They may return zero hits, but must not error.
	edgeCases := []struct {
		name  string
		query string
	}{
		{"unbalanced quote", `"databases`},
		{"bare AND operator", `AND`},
		{"bare OR operator", `OR`},
		{"bare NOT operator", `NOT`},
		{"asterisk wildcard", `data*`},
		{"unbalanced parens", `(databases`},
		{"special punctuation", `what's up?`},
	}
	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Search(tt.query, 50)
			if err != nil {
				t.Errorf("Search(%q) returned error: %v — should be sanitized", tt.query, err)
			}
		})
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("", 50)
	if err != nil {
		t.Errorf("Search(\"\") returned error: %v — empty query should return empty results", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(\"\") returned %d hits, want 0", len(hits))
	}
}

func TestSearchWhitespaceOnly(t *testing.T) {
	s := searchTestStore(t)
	hits, err := s.Search("   ", 50)
	if err != nil {
		t.Errorf("Search(whitespace) returned error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(whitespace) returned %d hits, want 0", len(hits))
	}
}

func TestSearchPhrasePreserved(t *testing.T) {
	s := searchTestStore(t)
	// Balanced quotes should be passed through as an FTS5 phrase query.
	// "deployment strategies" is a phrase that only appears in beta.md.
	hits, err := s.Search(`"deployment strategies"`, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "beta.md" {
		t.Errorf("phrase search returned %v, want single hit on beta.md", hits)
	}

	// "strategies deployment" is reversed — should NOT match as a phrase.
	hits, err = s.Search(`"strategies deployment"`, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("reversed phrase should return 0 hits, got %d", len(hits))
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
