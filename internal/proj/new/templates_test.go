package new

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseVars() TemplateVars {
	return TemplateVars{
		ProjectName: "test-project",
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Description: "A test project",
	}
}

func TestRenderAllCombos(t *testing.T) {
	t.Parallel()
	combos := []struct {
		name       string
		pkgManager string
		docSystem  string
		wantFiles  []string // files that must exist
		noFiles    []string // files that must NOT exist
	}{
		{
			name:       "pixi+quarto",
			pkgManager: "pixi",
			docSystem:  "quarto",
			wantFiles:  []string{"pyproject.toml", "_quarto.yml", "code/report.qmd", "code/templates/typst-show.typ", "code/templates/typst-template.typ", "README.md", ".gitignore", ".vscode/extensions.json", ".vscode/settings.json", ".zed/settings.json", ".zed/tasks.json", "code/bibliography.bib", "code/notebooks/analysis.py", "data/raw/penguins.csv"},
			noFiles:    []string{"myst.yml", "code/report.md"},
		},
		{
			name:       "pixi+myst",
			pkgManager: "pixi",
			docSystem:  "myst",
			wantFiles:  []string{"pyproject.toml", "myst.yml", "code/report.md", "README.md", ".gitignore", "code/bibliography.bib"},
			noFiles:    []string{"_quarto.yml", "code/report.qmd", "code/templates/typst-show.typ"},
		},
		{
			name:       "uv+quarto",
			pkgManager: "uv",
			docSystem:  "quarto",
			wantFiles:  []string{"pyproject.toml", "_quarto.yml", "code/report.qmd", "README.md"},
			noFiles:    []string{"myst.yml", "code/report.md"},
		},
		{
			name:       "uv+myst",
			pkgManager: "uv",
			docSystem:  "myst",
			wantFiles:  []string{"pyproject.toml", "myst.yml", "code/report.md", "README.md"},
			noFiles:    []string{"_quarto.yml", "code/report.qmd"},
		},
		{
			name:       "pixi+none",
			pkgManager: "pixi",
			docSystem:  "none",
			wantFiles:  []string{"pyproject.toml", "README.md", ".gitignore", ".vscode/settings.json", ".zed/settings.json", ".zed/tasks.json", "code/notebooks/analysis.py"},
			noFiles:    []string{"_quarto.yml", "myst.yml", "code/report.qmd", "code/report.md", "code/templates/typst-show.typ", "code/templates/typst-template.typ"},
		},
		{
			name:       "uv+none",
			pkgManager: "uv",
			docSystem:  "none",
			wantFiles:  []string{"pyproject.toml", "README.md", ".gitignore"},
			noFiles:    []string{"_quarto.yml", "myst.yml", "code/report.qmd", "code/report.md"},
		},
	}

	for _, tt := range combos {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := baseVars()
			vars.PkgManager = tt.pkgManager
			vars.DocSystem = tt.docSystem

			dest := t.TempDir()
			created, err := RenderAll(vars, dest)
			if err != nil {
				t.Fatalf("RenderAll failed: %v", err)
			}

			createdSet := make(map[string]bool)
			for _, f := range created {
				createdSet[f] = true
			}

			for _, want := range tt.wantFiles {
				if !createdSet[want] {
					t.Errorf("expected file %q to be created, got files: %v", want, created)
				}
				// Also verify it exists on disk
				if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
					t.Errorf("file %q not found on disk: %v", want, err)
				}
			}

			for _, noWant := range tt.noFiles {
				if createdSet[noWant] {
					t.Errorf("file %q should NOT be created for %s", noWant, tt.name)
				}
			}
		})
	}
}

func TestPyprojectContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		pkgManager string
		docSystem  string
		contains   []string
		notContain []string
	}{
		{
			name:       "pixi+quarto has pixi tasks and quarto render",
			pkgManager: "pixi",
			docSystem:  "quarto",
			contains:   []string{"[tool.pixi.workspace]", "[tool.pixi.tasks]", `render = { cmd = "quarto render"`, `extend-include = ["*.ipynb", "*.qmd"]`},
			notContain: []string{"[tool.poe", "docs-start", "nodejs"},
		},
		{
			name:       "pixi+myst has nodejs and docs tasks",
			pkgManager: "pixi",
			docSystem:  "myst",
			contains:   []string{"[tool.pixi.workspace]", `nodejs = ">=20.0.0"`, "docs-start", "docs-build"},
			notContain: []string{"[tool.poe", `quarto render`},
		},
		{
			name:       "uv+quarto has poe tasks",
			pkgManager: "uv",
			docSystem:  "quarto",
			contains:   []string{"[tool.poe.tasks.render]", "dependencies = [", "poethepoet"},
			notContain: []string{"[tool.pixi", "docs-start"},
		},
		{
			name:       "uv+none has no doc tasks",
			pkgManager: "uv",
			docSystem:  "none",
			contains:   []string{"[tool.poe.tasks.setup]", "[tool.poe.tasks.marimo]"},
			notContain: []string{"quarto render", "docs-start", "docs-build", "nodejs"},
		},
		{
			name:       "pixi+none has no doc tasks",
			pkgManager: "pixi",
			docSystem:  "none",
			contains:   []string{"[tool.pixi.tasks]", "[tool.pixi.tasks.marimo]"},
			notContain: []string{"quarto render", "docs-start", "docs-build", "nodejs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vars := baseVars()
			vars.PkgManager = tt.pkgManager
			vars.DocSystem = tt.docSystem

			content, err := RenderFile("pyproject.toml.tmpl", vars)
			if err != nil {
				t.Fatalf("RenderFile failed: %v", err)
			}

			for _, s := range tt.contains {
				if !strings.Contains(content, s) {
					t.Errorf("expected pyproject.toml to contain %q", s)
				}
			}
			for _, s := range tt.notContain {
				if strings.Contains(content, s) {
					t.Errorf("pyproject.toml should NOT contain %q", s)
				}
			}
		})
	}
}

func TestConditionalFileSkipping(t *testing.T) {
	t.Parallel()
	// _quarto.yml should render empty (and be skipped) when docSystem != "quarto"
	vars := baseVars()
	vars.PkgManager = "pixi"
	vars.DocSystem = "myst"

	content, err := RenderFile("_quarto.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if strings.TrimSpace(content) != "" {
		t.Errorf("_quarto.yml should render empty for myst, got: %q", content)
	}

	// myst.yml should render empty when docSystem != "myst"
	vars.DocSystem = "quarto"
	content, err = RenderFile("myst.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if strings.TrimSpace(content) != "" {
		t.Errorf("myst.yml should render empty for quarto, got: %q", content)
	}
}
