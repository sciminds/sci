package new

import (
	"testing"
)

func TestCreateDryRun(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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

func TestCreateWritingDryRun(t *testing.T) {
	t.Parallel()
	opts := CreateOptions{
		Name:        "paper",
		Dir:         t.TempDir(),
		Kind:        "writing",
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
	if len(result.Files) < 13 {
		t.Errorf("expected at least 13 files, got %d: %v", len(result.Files), result.Files)
	}

	got := make(map[string]bool)
	for _, f := range result.Files {
		got[f] = true
	}
	for _, want := range []string{"main.md", "myst.yml", "_templates/paper/paper.typ"} {
		if !got[want] {
			t.Errorf("missing expected file %q in %v", want, result.Files)
		}
	}
	for _, noWant := range []string{"pyproject.toml", "code/notebooks/analysis.py", "data/raw/penguins.csv"} {
		if got[noWant] {
			t.Errorf("file %q must not be created for writing kind", noWant)
		}
	}
}

func TestDefaultPostStepsWriting(t *testing.T) {
	t.Parallel()
	steps := DefaultPostSteps("writing", "")
	if len(steps) != 1 {
		t.Fatalf("writing kind should produce exactly one post-step (git init), got %d: %+v", len(steps), steps)
	}
	if steps[0].Label != "git init" {
		t.Errorf("expected git init, got %q", steps[0].Label)
	}
}

func TestDefaultPostStepsPython(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind       string
		pkgManager string
		wantLabels []string
	}{
		{"python", "pixi", []string{"git init", "pixi install"}},
		{"python", "uv", []string{"git init", "uv sync"}},
		{"", "pixi", []string{"git init", "pixi install"}}, // empty kind == python (back-compat)
	}
	for _, tt := range tests {
		t.Run(tt.kind+"+"+tt.pkgManager, func(t *testing.T) {
			t.Parallel()
			steps := DefaultPostSteps(tt.kind, tt.pkgManager)
			if len(steps) != len(tt.wantLabels) {
				t.Fatalf("got %d steps, want %d: %+v", len(steps), len(tt.wantLabels), steps)
			}
			for i, label := range tt.wantLabels {
				if steps[i].Label != label {
					t.Errorf("step %d: got %q, want %q", i, steps[i].Label, label)
				}
			}
		})
	}
}

func TestCreateDirExists(t *testing.T) {
	t.Parallel()
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
