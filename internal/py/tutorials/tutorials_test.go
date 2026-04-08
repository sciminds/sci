package tutorials

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewritePaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "../../data prefix",
			input: `pd.read_csv("../../data/tutorials/advertising.csv")`,
			want:  `pd.read_csv("data/tutorials/advertising.csv")`,
		},
		{
			name:  "../../figs prefix",
			input: `<img src="../../figs/tutorials/seaborn.png">`,
			want:  `<img src="figs/tutorials/seaborn.png">`,
		},
		{
			name:  "../../../data prefix",
			input: `read("../../../data/tutorials/credit.csv")`,
			want:  `read("data/tutorials/credit.csv")`,
		},
		{
			name:  ".../../data prefix",
			input: `read(".../../data/tutorials/example.csv")`,
			want:  `read("data/tutorials/example.csv")`,
		},
		{
			name:  "no match left alone",
			input: `import marimo`,
			want:  `import marimo`,
		},
		{
			name:  "multiple on same line",
			input: `a("../../data/tutorials/a.csv"); b("../../figs/tutorials/b.png")`,
			want:  `a("data/tutorials/a.csv"); b("figs/tutorials/b.png")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewritePaths(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.py")
	_ = os.WriteFile(path, []byte(`pd.read_csv("../../data/tutorials/ad.csv")`), 0o644)

	if err := rewriteFile(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	want := `pd.read_csv("data/tutorials/ad.csv")`
	if string(data) != want {
		t.Errorf("got %q, want %q", string(data), want)
	}
}

func TestRewriteFileNoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helpers.py")
	original := `def helper(): pass`
	_ = os.WriteFile(path, []byte(original), 0o644)

	if err := rewriteFile(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("file was modified unexpectedly: %q", string(data))
	}
}

func TestFindByName(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		got := findByName("03-seaborn")
		if got == nil || got.Title != "Seaborn" {
			t.Errorf("findByName(03-seaborn) = %v", got)
		}
	})

	t.Run("helpers", func(t *testing.T) {
		got := findByName("helpers")
		if got == nil || got.File != "helpers.py" {
			t.Errorf("findByName(helpers) = %v", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := findByName("nonexistent")
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestManifestConsistency(t *testing.T) {
	seen := make(map[string]bool)
	for _, tut := range Manifest {
		if tut.Name == "" || tut.Title == "" || tut.File == "" {
			t.Errorf("empty field in %+v", tut)
		}
		if seen[tut.Name] {
			t.Errorf("duplicate name: %s", tut.Name)
		}
		seen[tut.Name] = true

		if !strings.HasSuffix(tut.File, ".py") {
			t.Errorf("file should end in .py: %s", tut.File)
		}
	}
}

func TestExtractZip(t *testing.T) {
	// Create a test zip with nested structure.
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(zf)
	f, _ := w.Create("data/tutorials/test.csv")
	_, _ = f.Write([]byte("a,b\n1,2\n"))
	f2, _ := w.Create("nested/deep/file.txt")
	_, _ = f2.Write([]byte("hello"))
	_ = w.Close()
	_ = zf.Close()

	destDir := filepath.Join(dir, "extracted")
	_ = os.MkdirAll(destDir, 0o755)
	if err := extractZip(zipPath, destDir); err != nil {
		t.Fatal(err)
	}

	// Verify files were extracted.
	csvData, err := os.ReadFile(filepath.Join(destDir, "data", "tutorials", "test.csv"))
	if err != nil {
		t.Fatalf("csv not extracted: %v", err)
	}
	if string(csvData) != "a,b\n1,2\n" {
		t.Errorf("csv contents = %q", string(csvData))
	}

	txtData, err := os.ReadFile(filepath.Join(destDir, "nested", "deep", "file.txt"))
	if err != nil {
		t.Fatalf("txt not extracted: %v", err)
	}
	if string(txtData) != "hello" {
		t.Errorf("txt contents = %q", string(txtData))
	}
}

func TestExtractZipSlipProtection(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	zf, _ := os.Create(zipPath)
	w := zip.NewWriter(zf)
	// Manually create a zip entry with path traversal.
	f, _ := w.Create("../../../etc/evil.txt")
	_, _ = f.Write([]byte("pwned"))
	_ = w.Close()
	_ = zf.Close()

	destDir := filepath.Join(dir, "safe")
	_ = os.MkdirAll(destDir, 0o755)
	err := extractZip(zipPath, destDir)
	if err == nil {
		t.Fatal("expected error for zip slip attack")
	}
	if !strings.Contains(err.Error(), "illegal path") {
		t.Errorf("error should mention illegal path: %v", err)
	}
}
