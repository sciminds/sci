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
			wantFiles:  []string{"pyproject.toml", "_quarto.yml", "code/report.qmd", "code/templates/typst-show.typ", "code/templates/typst-template.typ", "README.md", ".gitignore", ".vscode/extensions.json", ".vscode/settings.json", ".zed/settings.json", ".zed/tasks.json", "code/refs.bib", "code/notebooks/analysis.py", "data/raw/penguins.csv", "figs/.gitkeep", "data/derivatives/.gitkeep"},
			noFiles:    []string{"myst.yml", "code/report.md", "code/bibliography.bib"},
		},
		{
			name:       "pixi+myst",
			pkgManager: "pixi",
			docSystem:  "myst",
			wantFiles:  []string{"pyproject.toml", "myst.yml", "main.md", "refs.bib", "README.md", ".gitignore", "_templates/paper/paper.typ", "code/notebooks/analysis.py", "data/raw/penguins.csv", "figs/.gitkeep", "data/derivatives/.gitkeep"},
			noFiles:    []string{"_quarto.yml", "code/report.qmd", "code/report.md", "code/bibliography.bib", "code/refs.bib", "code/templates/typst-show.typ"},
		},
		{
			name:       "uv+quarto",
			pkgManager: "uv",
			docSystem:  "quarto",
			wantFiles:  []string{"pyproject.toml", "_quarto.yml", "code/report.qmd", "README.md", "code/refs.bib", "figs/.gitkeep", "data/derivatives/.gitkeep"},
			noFiles:    []string{"myst.yml", "code/report.md", "code/bibliography.bib"},
		},
		{
			name:       "uv+myst",
			pkgManager: "uv",
			docSystem:  "myst",
			wantFiles:  []string{"pyproject.toml", "myst.yml", "main.md", "refs.bib", "README.md", "_templates/paper/paper.typ", "figs/.gitkeep", "data/derivatives/.gitkeep"},
			noFiles:    []string{"_quarto.yml", "code/report.qmd", "code/report.md", "code/bibliography.bib"},
		},
		{
			name:       "pixi+none",
			pkgManager: "pixi",
			docSystem:  "none",
			wantFiles:  []string{"pyproject.toml", "README.md", ".gitignore", ".vscode/settings.json", ".zed/settings.json", ".zed/tasks.json", "code/notebooks/analysis.py", "figs/.gitkeep", "data/derivatives/.gitkeep"},
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

// Default writing project = single-file layout + lab template.
func TestRenderAllWriting(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	dest := t.TempDir()
	created, err := RenderAll(vars, dest)
	if err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	createdSet := make(map[string]bool)
	for _, f := range created {
		createdSet[f] = true
	}

	wantFiles := []string{
		"README.md",
		"myst.yml",
		"main.md",
		"refs.bib",
		".gitignore",
		"figs/.gitkeep",
		"_templates/paper/paper.typ",
		"_templates/paper/template.yml",
		"_templates/paper/orcid.svg",
	}
	for _, want := range wantFiles {
		if !createdSet[want] {
			t.Errorf("expected file %q to be created, got files: %v", want, created)
		}
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("file %q not found on disk: %v", want, err)
		}
	}

	noFiles := []string{
		"pyproject.toml",
		"_quarto.yml",
		"code/report.md",
		"code/report.qmd",
		"code/notebooks/analysis.py",
		"data/raw/penguins.csv",
		"references.bib",       // renamed → refs.bib
		"figures/.gitkeep",     // renamed → figs/
		"sections/abstract.md", // single-file: parts inline in main.md
	}
	for _, noWant := range noFiles {
		if createdSet[noWant] {
			t.Errorf("file %q should NOT be created for writing kind", noWant)
		}
	}
}

func TestRenderAllWritingComposed(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.MdLayout = "composed"

	dest := t.TempDir()
	created, err := RenderAll(vars, dest)
	if err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	createdSet := make(map[string]bool)
	for _, f := range created {
		createdSet[f] = true
	}

	wantFiles := []string{
		"main.md",
		"myst.yml",
		"sections/abstract.md",
		"sections/keypoints.md",
		"sections/acknowledgements.md",
		"sections/opendata.md",
		"_templates/paper/paper.typ",
	}
	for _, want := range wantFiles {
		if !createdSet[want] {
			t.Errorf("expected file %q to be created, got files: %v", want, created)
		}
	}
}

func TestRenderAllWritingTemplateDefault(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.Template = "default"

	dest := t.TempDir()
	if _, err := RenderAll(vars, dest); err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "_templates/paper/paper.typ")); err == nil {
		t.Errorf("_templates/paper/paper.typ should NOT be created when Template=default")
	}

	mystYml, err := os.ReadFile(filepath.Join(dest, "myst.yml"))
	if err != nil {
		t.Fatalf("read myst.yml: %v", err)
	}
	// Export block should not pin a template name (the site: block has its
	// own template: book-theme — that's HTML site theming, not the typst
	// export template, so we only check the `format: typst` block.)
	exportIdx := strings.Index(string(mystYml), "- format: typst")
	siteIdx := strings.Index(string(mystYml), "\nsite:")
	if exportIdx == -1 || siteIdx == -1 {
		t.Fatalf("could not find export/site block in myst.yml:\n%s", mystYml)
	}
	exportBlock := string(mystYml)[exportIdx:siteIdx]
	if strings.Contains(exportBlock, "template:") {
		t.Errorf("typst export block should not contain template: for default template, got:\n%s", exportBlock)
	}
}

func TestRenderAllWritingTemplateNamed(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.Template = "lapreprint-typst"

	dest := t.TempDir()
	if _, err := RenderAll(vars, dest); err != nil {
		t.Fatalf("RenderAll failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest, "_templates/paper/paper.typ")); err == nil {
		t.Errorf("_templates/paper/paper.typ should NOT be created when Template=lapreprint-typst")
	}

	mystYml, err := os.ReadFile(filepath.Join(dest, "myst.yml"))
	if err != nil {
		t.Fatalf("read myst.yml: %v", err)
	}
	if !strings.Contains(string(mystYml), "template: lapreprint-typst") {
		t.Errorf("myst.yml should contain 'template: lapreprint-typst', got:\n%s", mystYml)
	}
}

// Single-file layout (default): myst.yml has no parts: block; main.md
// frontmatter carries the parts inline.
func TestWritingMystYmlContent(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	content, err := RenderFile("myst.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, want := range []string{
		"template: ./_templates/paper",
		"test-project",
		"Test Author",
		"test@example.com",
		"file: main.md",
		"output: pdfs/main.pdf",
		"refs.bib",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("myst.yml missing %q\n--- content ---\n%s", want, content)
		}
	}
	for _, notWant := range []string{
		"sections/abstract.md",
		"sections/keypoints.md",
	} {
		if strings.Contains(content, notWant) {
			t.Errorf("single-file myst.yml should not reference %q\n--- content ---\n%s", notWant, content)
		}
	}
}

// Composed layout: myst.yml has parts: pointing at sections/*.md.
func TestWritingMystYmlComposedContent(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.MdLayout = "composed"

	content, err := RenderFile("myst.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, want := range []string{
		"abstract: sections/abstract.md",
		"keypoints: sections/keypoints.md",
		"acknowledgements: sections/acknowledgements.md",
		"data_availability: sections/opendata.md",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("composed myst.yml missing %q\n--- content ---\n%s", want, content)
		}
	}
}

// Single-file main.md: frontmatter contains all parts inline.
func TestWritingMainMdContent(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	content, err := RenderFile("main.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("main.md must start with frontmatter, got:\n%s", content)
	}
	for _, want := range []string{
		"title: test-project",
		"abstract:",
		"keypoints:",
		"acknowledgements:",
		"data_availability:",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("single-file main.md missing %q", want)
		}
	}
}

// Composed main.md: frontmatter only carries title (no parts inline).
func TestWritingMainMdComposedContent(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.MdLayout = "composed"

	content, err := RenderFile("main.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if !strings.Contains(content, "title: test-project") {
		t.Errorf("composed main.md missing title")
	}
	if strings.Contains(content, "abstract:") {
		t.Errorf("composed main.md should not contain abstract: in frontmatter (lives in sections/abstract.md)")
	}
}

func TestWritingReadmeContent(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	content, err := RenderFile("README.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, want := range []string{
		"test-project",
		"mystmd build --pdf",
		"_templates/paper/paper.typ",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
}

// Single-file writing README: parts live inline in main.md frontmatter, so
// the structure block must not list sections/*.md files.
func TestWritingReadmeSingleFileNoSections(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	content, err := RenderFile("README.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, notWant := range []string{
		"sections/abstract.md",
		"sections/keypoints.md",
		"sections/acknowledgements.md",
		"sections/opendata.md",
	} {
		if strings.Contains(content, notWant) {
			t.Errorf("single-file README should not mention %q\n--- content ---\n%s", notWant, content)
		}
	}
	if !strings.Contains(content, "inline in frontmatter") {
		t.Errorf("single-file README should describe inline frontmatter parts\n--- content ---\n%s", content)
	}
}

// Composed writing README: sections/*.md files exist on disk, so the
// structure block must list them.
func TestWritingReadmeComposedHasSections(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.MdLayout = "composed"

	content, err := RenderFile("README.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, want := range []string{
		"sections/",
		"abstract.md",
		"keypoints.md",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("composed README missing %q\n--- content ---\n%s", want, content)
		}
	}
}

// Non-lab template writing README: _templates/paper/ doesn't ship, so the
// README must not point at it.
func TestWritingReadmeNonLabTemplate(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"
	vars.Template = "default"

	content, err := RenderFile("README.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if strings.Contains(content, "_templates/paper") {
		t.Errorf("non-lab README should not mention _templates/paper\n--- content ---\n%s", content)
	}
	if !strings.Contains(content, "default") {
		t.Errorf("non-lab README should name the template (default)\n--- content ---\n%s", content)
	}
}

// Non-lab template python README: _templates/paper/ doesn't ship, so the
// project-structure block must not list it.
func TestPythonReadmeNonLabTemplate(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "python"
	vars.PkgManager = "uv"
	vars.DocSystem = "myst"
	vars.Template = "lapreprint-typst"

	content, err := RenderFile("README.md.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	if strings.Contains(content, "_templates/paper") {
		t.Errorf("non-lab python README should not mention _templates/paper\n--- content ---\n%s", content)
	}
}

// python+myst myst.yml must carry the notebook-output cleanup settings so
// rendered manuscripts don't leak `<Figure size 640x480>` repr strings.
func TestPythonMystYmlNotebookSettings(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "python"
	vars.PkgManager = "uv"
	vars.DocSystem = "myst"

	content, err := RenderFile("myst.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, want := range []string{
		"output_matplotlib_strings: remove",
		"output_stderr: remove-error",
		"_build/**",
		"**/.jupyter_cache/**",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("python+myst myst.yml missing %q\n--- content ---\n%s", want, content)
		}
	}
}

// Writing myst.yml must NOT carry the python notebook settings — they only
// apply when there are notebooks to clean up.
func TestWritingMystYmlNoNotebookSettings(t *testing.T) {
	t.Parallel()
	vars := baseVars()
	vars.Kind = "writing"

	content, err := RenderFile("myst.yml.tmpl", vars)
	if err != nil {
		t.Fatalf("RenderFile failed: %v", err)
	}
	for _, notWant := range []string{
		"output_matplotlib_strings",
		"output_stderr",
		".jupyter_cache",
	} {
		if strings.Contains(content, notWant) {
			t.Errorf("writing myst.yml should not contain %q\n--- content ---\n%s", notWant, content)
		}
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

	// myst.yml lives in the _paper overlay, which is only included when the
	// project actually has a manuscript (writing or python+myst). For
	// python+quarto, no overlay ships myst.yml.tmpl — RenderFile must return
	// fs.ErrNotExist rather than rendering anything.
	vars.DocSystem = "quarto"
	if _, err := RenderFile("myst.yml.tmpl", vars); err == nil {
		t.Error("expected myst.yml.tmpl to be unresolvable for python+quarto")
	}
}
