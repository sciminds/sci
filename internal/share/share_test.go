package share

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShare_RequiresAuth(t *testing.T) {
	t.Setenv("SCI_CONFIG_PATH", filepath.Join(t.TempDir(), "no-such-config.json"))
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.csv")
	if err := os.WriteFile(f, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Share(f, ShareOpts{Name: "test.csv"})
	if err == nil {
		t.Fatal("expected error (no config), got nil")
	}
}

func TestGet_RequiresAuth(t *testing.T) {
	t.Setenv("SCI_CONFIG_PATH", filepath.Join(t.TempDir(), "no-such-config.json"))
	_, err := Get("nonexistent-file.csv", false)
	if err == nil {
		t.Fatal("expected error (no config), got nil")
	}
}

func TestCheckExists_RequiresAuth(t *testing.T) {
	t.Setenv("SCI_CONFIG_PATH", filepath.Join(t.TempDir(), "no-such-config.json"))
	_, err := CheckExists("test.csv", false)
	if err == nil {
		t.Fatal("expected error (no config), got nil")
	}
}

func TestDefaultShareName(t *testing.T) {
	// Regular file.
	tmp := t.TempDir()
	f := filepath.Join(tmp, "results.csv")
	if err := os.WriteFile(f, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DefaultShareName(f); got != "results.csv" {
		t.Errorf("DefaultShareName(file) = %q, want %q", got, "results.csv")
	}

	// Directory.
	d := filepath.Join(tmp, "mydata")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DefaultShareName(d); got != "mydata.zip" {
		t.Errorf("DefaultShareName(dir) = %q, want %q", got, "mydata.zip")
	}

	// Non-existent path falls back to base name.
	if got := DefaultShareName("/no/such/file.db"); got != "file.db" {
		t.Errorf("DefaultShareName(missing) = %q, want %q", got, "file.db")
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data.csv", "csv"},
		{"data.tsv", "csv"},
		{"backup.db", "db"},
		{"photo.png", "media"},
		{"archive.zip", "zip"},
		{"readme.txt", "other"},
	}
	for _, tt := range tests {
		if got := detectFileType(tt.path); got != tt.want {
			t.Errorf("detectFileType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data.csv", "text/csv"},
		{"data.json", "application/json"},
		{"photo.png", "image/png"},
		{"archive.zip", "application/zip"},
		{"unknown.xyz", "application/octet-stream"},
	}
	for _, tt := range tests {
		if got := detectContentType(tt.path); got != tt.want {
			t.Errorf("detectContentType(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestNameFromFile(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/results.csv", "results"},
		{"data.tar.gz", "data.tar"},
		{"mydir", "mydir"},
	}
	for _, tt := range tests {
		if got := nameFromFile(tt.path); got != tt.want {
			t.Errorf("nameFromFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
