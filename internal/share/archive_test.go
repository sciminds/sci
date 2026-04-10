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

func TestZipDir_EmptyFiles(t *testing.T) {
	src := t.TempDir()
	// Create an empty file (zero bytes).
	if err := os.WriteFile(filepath.Join(src, "empty.txt"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := zipDir(src, dest); err != nil {
		t.Fatalf("zipDir with empty file: %v", err)
	}

	// Unzip and verify empty file is preserved.
	out := t.TempDir()
	if err := unzip(dest, out); err != nil {
		t.Fatalf("unzip: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, "empty.txt"))
	if err != nil {
		t.Fatalf("empty.txt not extracted: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestZipDir_UnicodeFilenames(t *testing.T) {
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "données.csv"), []byte("a,b\n1,2\n"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "日本語.txt"), []byte("hello"), 0o644)

	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := zipDir(src, dest); err != nil {
		t.Fatalf("zipDir with unicode filenames: %v", err)
	}

	out := t.TempDir()
	if err := unzip(dest, out); err != nil {
		t.Fatalf("unzip: %v", err)
	}

	// Verify both files survived the round-trip.
	data, err := os.ReadFile(filepath.Join(out, "données.csv"))
	if err != nil {
		t.Fatalf("données.csv not extracted: %v", err)
	}
	if string(data) != "a,b\n1,2\n" {
		t.Errorf("données.csv content = %q", data)
	}
	data, err = os.ReadFile(filepath.Join(out, "日本語.txt"))
	if err != nil {
		t.Fatalf("日本語.txt not extracted: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("日本語.txt content = %q", data)
	}
}

func TestUnzip_CorruptFile(t *testing.T) {
	// A file that's not a valid zip should return an error.
	corrupt := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(corrupt, []byte("this is not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := unzip(corrupt, t.TempDir())
	if err == nil {
		t.Fatal("expected error for corrupt zip, got nil")
	}
}

func TestUnzip_NonexistentFile(t *testing.T) {
	err := unzip("/nonexistent/file.zip", t.TempDir())
	if err == nil {
		t.Fatal("expected error for nonexistent zip, got nil")
	}
}

func TestFileManifest_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	manifest := fileManifest(dir)
	if manifest != "" {
		t.Errorf("expected empty manifest for empty dir, got %q", manifest)
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
