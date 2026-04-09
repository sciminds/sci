package share

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/cloud"
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
	_, err := Get("nonexistent-file.csv")
	if err == nil {
		t.Fatal("expected error (no config), got nil")
	}
}

func TestCheckExists_RequiresAuth(t *testing.T) {
	t.Setenv("SCI_CONFIG_PATH", filepath.Join(t.TempDir(), "no-such-config.json"))
	_, err := CheckExists("test.csv")
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

func TestBuildSharedEntries_OwnFiles(t *testing.T) {
	objects := []cloud.ObjectInfo{
		{Key: "alice/iris.csv", Size: 1024, LastModified: "2024-01-01T00:00:00Z", URL: "https://pub.r2.dev/alice/iris.csv"},
		{Key: "alice/titanic.db", Size: 2048, LastModified: "2024-02-01T00:00:00Z", URL: "https://pub.r2.dev/alice/titanic.db"},
	}

	entries := buildSharedEntries(objects, "alice", false)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	// Owner should be empty when listing own files.
	for _, e := range entries {
		if e.Owner != "" {
			t.Errorf("Owner = %q, want empty for own-files mode", e.Owner)
		}
	}
	if entries[0].Name != "iris.csv" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "iris.csv")
	}
	if entries[1].Name != "titanic.db" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "titanic.db")
	}
	if entries[0].Type != "csv" {
		t.Errorf("entries[0].Type = %q, want %q", entries[0].Type, "csv")
	}
	if entries[1].Type != "db" {
		t.Errorf("entries[1].Type = %q, want %q", entries[1].Type, "db")
	}
}

func TestBuildSharedEntries_AllUsers(t *testing.T) {
	objects := []cloud.ObjectInfo{
		{Key: "alice/iris.csv", Size: 1024, LastModified: "2024-01-01T00:00:00Z", URL: "https://pub.r2.dev/alice/iris.csv"},
		{Key: "bob/penguins.csv", Size: 4096, LastModified: "2024-03-01T00:00:00Z", URL: "https://pub.r2.dev/bob/penguins.csv"},
		{Key: "alice/results.db", Size: 2048, LastModified: "2024-02-01T00:00:00Z", URL: "https://pub.r2.dev/alice/results.db"},
	}

	entries := buildSharedEntries(objects, "alice", true)

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// All entries should have Owner set.
	if entries[0].Owner != "alice" {
		t.Errorf("entries[0].Owner = %q, want %q", entries[0].Owner, "alice")
	}
	if entries[1].Owner != "bob" {
		t.Errorf("entries[1].Owner = %q, want %q", entries[1].Owner, "bob")
	}
	if entries[2].Owner != "alice" {
		t.Errorf("entries[2].Owner = %q, want %q", entries[2].Owner, "alice")
	}
	// Names should be filename only, not full key.
	if entries[0].Name != "iris.csv" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "iris.csv")
	}
	if entries[1].Name != "penguins.csv" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "penguins.csv")
	}
}

func TestBuildSharedEntries_AllUsers_PreservesFields(t *testing.T) {
	objects := []cloud.ObjectInfo{
		{Key: "bob/data.csv", Size: 999, LastModified: "2024-06-15T12:00:00Z", URL: "https://pub.r2.dev/bob/data.csv"},
	}

	entries := buildSharedEntries(objects, "alice", true)

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Size != 999 {
		t.Errorf("Size = %d, want 999", e.Size)
	}
	if e.Updated != "2024-06-15T12:00:00Z" {
		t.Errorf("Updated = %q, want %q", e.Updated, "2024-06-15T12:00:00Z")
	}
	if e.URL != "https://pub.r2.dev/bob/data.csv" {
		t.Errorf("URL = %q, want %q", e.URL, "https://pub.r2.dev/bob/data.csv")
	}
	if e.Type != "csv" {
		t.Errorf("Type = %q, want %q", e.Type, "csv")
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
