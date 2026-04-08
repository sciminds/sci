package share

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZipDir(t *testing.T) {
	// Create a directory structure to zip.
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o644)

	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := zipDir(src, dest); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("zip file is empty")
	}
}

func TestUnzip(t *testing.T) {
	// Create a source dir, zip it, then unzip to a new location.
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "data"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "readme.txt"), []byte("top-level"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "data", "file.csv"), []byte("a,b\n1,2"), 0o644)

	zipPath := filepath.Join(t.TempDir(), "test.zip")
	if err := zipDir(src, zipPath); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if err := unzip(zipPath, out); err != nil {
		t.Fatal(err)
	}

	// Verify files were extracted with structure preserved.
	data, err := os.ReadFile(filepath.Join(out, "readme.txt"))
	if err != nil {
		t.Fatalf("readme.txt not extracted: %v", err)
	}
	if string(data) != "top-level" {
		t.Errorf("readme.txt = %q", data)
	}

	data, err = os.ReadFile(filepath.Join(out, "data", "file.csv"))
	if err != nil {
		t.Fatalf("data/file.csv not extracted: %v", err)
	}
	if string(data) != "a,b\n1,2" {
		t.Errorf("data/file.csv = %q", data)
	}
}

func TestFileManifest(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("world"), 0o644)

	manifest := fileManifest(dir)
	if !strings.Contains(manifest, "a.txt") {
		t.Errorf("manifest missing a.txt: %s", manifest)
	}
	if !strings.Contains(manifest, "sub/b.txt") {
		t.Errorf("manifest missing sub/b.txt: %s", manifest)
	}
}
