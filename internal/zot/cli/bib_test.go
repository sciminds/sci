package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFiles(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCollectBibTargets_File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "paper.qmd")
	got, err := collectBibTargets(filepath.Join(dir, "paper.qmd"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "paper.qmd" {
		t.Errorf("got %v", got)
	}
}

func TestCollectBibTargets_DirNonRecursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "b.md", "a.qmd", "notes.txt", "sub/deep.md")
	got, err := collectBibTargets(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.qmd"), filepath.Join(dir, "b.md")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectBibTargets_Recursive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFiles(t, dir, "a.md", "sub/deep.md", ".obsidian/config.md", "sub/skip.txt")
	got, err := collectBibTargets(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "sub", "deep.md")}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v (hidden dirs must be skipped)", got, want)
	}
}

func TestCollectBibTargets_RMarkdownAndCase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// R Markdown conventionally capitalizes the extension (.Rmd); the
	// sweep must be case-insensitive so neither casing is skipped.
	writeFiles(t, dir, "analysis.Rmd", "notes.rmd", "PAPER.MD", "skip.txt")
	got, err := collectBibTargets(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "PAPER.MD"),
		filepath.Join(dir, "analysis.Rmd"),
		filepath.Join(dir, "notes.rmd"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
