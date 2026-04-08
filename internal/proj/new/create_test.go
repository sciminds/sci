package new

import (
	"testing"
)

func TestCreateDryRun(t *testing.T) {
	combos := []struct {
		pkgManager string
		docSystem  string
		minFiles   int
	}{
		{"pixi", "quarto", 10},
		{"pixi", "myst", 8},
		{"uv", "quarto", 10},
		{"uv", "myst", 8},
		{"pixi", "none", 6},
		{"uv", "none", 6},
	}

	for _, tt := range combos {
		name := tt.pkgManager + "+" + tt.docSystem
		t.Run(name, func(t *testing.T) {
			opts := CreateOptions{
				Name:        "test-proj",
				Dir:         t.TempDir(),
				PkgManager:  tt.pkgManager,
				DocSystem:   tt.docSystem,
				AuthorName:  "Test",
				AuthorEmail: "test@test.com",
				DryRun:      true,
			}

			result, err := Create(opts)
			if err != nil {
				t.Fatalf("Create failed: %v", err)
			}
			if !result.DryRun {
				t.Error("expected DryRun to be true")
			}
			if len(result.Files) < tt.minFiles {
				t.Errorf("expected at least %d files, got %d: %v", tt.minFiles, len(result.Files), result.Files)
			}
		})
	}
}

func TestCreateDirExists(t *testing.T) {
	dir := t.TempDir()
	opts := CreateOptions{
		Name:       ".",
		Dir:        dir,
		PkgManager: "pixi",
		DocSystem:  "none",
		DryRun:     true,
	}
	// dir/. already exists, should error
	_, err := Create(opts)
	if err == nil {
		t.Fatal("expected error when dir already exists")
	}
}
