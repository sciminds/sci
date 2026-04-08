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
