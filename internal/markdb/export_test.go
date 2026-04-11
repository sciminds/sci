package markdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportRoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	srcDir := t.TempDir()

	files := map[string]string{
		"simple.md":     "Just plain text\nwith multiple lines",
		"with-fm.md":    "---\ntitle: Hello World\ntags: [a, b]\n---\nBody content here",
		"sub/nested.md": "---\ncategory: deep\n---\nNested file",
		"no-fm.md":      "No frontmatter at all",
	}
	for path, content := range files {
		writeFile(t, srcDir, path, content)
	}

	if _, err := s.Ingest(srcDir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	stats, err := s.Export(outDir, "")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if stats.Written != len(files) {
		t.Errorf("Written = %d, want %d", stats.Written, len(files))
	}

	// Verify byte-identical round-trip.
	for relPath, wantContent := range files {
		got, err := os.ReadFile(filepath.Join(outDir, relPath))
		if err != nil {
			t.Errorf("read %q: %v", relPath, err)
			continue
		}
		if string(got) != wantContent {
			t.Errorf("%q: content mismatch\ngot:  %q\nwant: %q", relPath, string(got), wantContent)
		}
	}
}

func TestExportWithFrontmatter(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	content := "---\ntitle: Test\n---\nBody"
	writeFile(t, dir, "note.md", content)

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if _, err := s.Export(outDir, ""); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(outDir, "note.md"))
	if string(got) != content {
		t.Errorf("got %q, want %q", string(got), content)
	}
}

func TestExportWithoutFrontmatter(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	content := "Just body"
	writeFile(t, dir, "note.md", content)

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if _, err := s.Export(outDir, ""); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(outDir, "note.md"))
	if string(got) != content {
		t.Errorf("got %q, want %q", string(got), content)
	}
}

func TestExportCreatesSubdirs(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "a/b/c/deep.md", "deep content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if _, err := s.Export(outDir, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outDir, "a", "b", "c", "deep.md")); err != nil {
		t.Errorf("deep file not found: %v", err)
	}
}

func TestExportWithWhereFilter(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "---\ntitle: Alpha\n---\nA content")
	writeFile(t, dir, "b.md", "---\ntitle: Beta\n---\nB content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	stats, err := s.Export(outDir, "title = 'Alpha'")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Written != 1 {
		t.Errorf("Written = %d, want 1", stats.Written)
	}

	if _, err := os.Stat(filepath.Join(outDir, "a.md")); err != nil {
		t.Error("a.md not exported")
	}
	if _, err := os.Stat(filepath.Join(outDir, "b.md")); !os.IsNotExist(err) {
		t.Error("b.md should not be exported")
	}
}

func TestExportNoMatches(t *testing.T) {
	t.Parallel()
	s, _ := ingestTestStore(t)
	dir := t.TempDir()
	writeFile(t, dir, "note.md", "content")

	if _, err := s.Ingest(dir); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	stats, err := s.Export(outDir, "path = 'nonexistent.md'")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Written != 0 {
		t.Errorf("Written = %d, want 0", stats.Written)
	}
}
