package new

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Integration tests gated behind SLOW=1.
// These create real pixi/uv environments in temp dirs, run notebooks,
// and render documents. They require pixi, uv, quarto, marimo, typst,
// node, and npm on PATH.
//
// Run: SLOW=1 go test ./internal/py/new -run Integration -v -timeout 10m

func skipUnlessSlow(t *testing.T) {
	t.Helper()
	if os.Getenv("SLOW") == "" {
		t.Skip("skipping integration test (set SLOW=1 to run)")
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("skipping: %s not found on PATH", name)
	}
}

// createProject scaffolds a real project into a temp dir, runs post-steps
// (git init + pixi install / uv sync), and returns the project path.
func createProject(t *testing.T, pkgManager, docSystem string) string {
	t.Helper()

	dir := t.TempDir()
	name := "test-" + pkgManager + "-" + docSystem

	result, err := Create(CreateOptions{
		Name:        name,
		Dir:         dir,
		PkgManager:  pkgManager,
		DocSystem:   docSystem,
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Description: "Integration test project",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatal("no files created")
	}

	return result.ProjectDir
}

// runIn executes a command in the given directory and returns combined
// stdout+stderr. Fails the test on non-zero exit.
func runIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed:\n%s\n%v", strings.Join(args, " "), out, err)
	}
	return string(out)
}

// runMarimoExport runs marimo export from the notebooks directory so that
// relative paths in the notebook (../../data/raw/) resolve correctly.
// Tolerates non-zero exit because mo.md() cells always fail in export mode.
func runMarimoExport(t *testing.T, dir string, pkgManager string) {
	t.Helper()
	nbDir := filepath.Join(dir, "code", "notebooks")
	nbPath := filepath.Join(nbDir, "analysis.py")

	var cmd *exec.Cmd
	switch pkgManager {
	case "pixi":
		// -x bypasses the pixi "marimo" task (which expects a name arg)
		cmd = exec.Command("pixi", "run", "-x",
			"--manifest-path", filepath.Join(dir, "pyproject.toml"),
			"marimo", "export", "html", nbPath,
			"-o", "/dev/null", "--no-include-code")
	case "uv":
		cmd = exec.Command("uv", "run", "--project", dir,
			"marimo", "export", "html", nbPath,
			"-o", "/dev/null", "--no-include-code")
	}
	cmd.Dir = nbDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// marimo exits non-zero when mo.md() cells fail — that's expected.
		// Log but don't fail; we assert on the figure file instead.
		t.Logf("marimo export exited with error (expected for mo.md cells):\n%s", out)
	}
}

// assertFileExists fails the test if the file does not exist.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

// --- pixi × quarto ---

func TestIntegrationPixiQuarto(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "pixi")
	requireTool(t, "quarto")
	requireTool(t, "typst")

	dir := createProject(t, "pixi", "quarto")

	t.Run("pixi install creates environment", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".pixi"))
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "pixi")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})

	t.Run("quarto renders PDF via typst", func(t *testing.T) {
		runIn(t, dir, "pixi", "run", "render")
		assertFileExists(t, filepath.Join(dir, "pdfs", "code", "report.pdf"))
	})
}

// --- pixi × myst ---

func TestIntegrationPixiMyst(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "pixi")
	requireTool(t, "typst")
	requireTool(t, "node")

	dir := createProject(t, "pixi", "myst")

	t.Run("pixi install creates environment", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".pixi"))
	})

	t.Run("setup registers jupyter kernel", func(t *testing.T) {
		runIn(t, dir, "pixi", "run", "setup")
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "pixi")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})

	t.Run("myst builds HTML site", func(t *testing.T) {
		runIn(t, dir, "pixi", "run", "docs-build")
		assertFileExists(t, filepath.Join(dir, "_build", "html"))
	})

	t.Run("myst exports PDF via typst", func(t *testing.T) {
		runIn(t, dir, "pixi", "run", "docs-pdf")
		assertFileExists(t, filepath.Join(dir, "pdfs", "report.pdf"))
	})
}

// --- uv × quarto ---

func TestIntegrationUvQuarto(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "uv")
	requireTool(t, "quarto")
	requireTool(t, "typst")

	dir := createProject(t, "uv", "quarto")

	t.Run("uv sync creates virtualenv", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".venv"))
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "uv")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})

	t.Run("quarto renders PDF via typst", func(t *testing.T) {
		runIn(t, dir, "uv", "run", "poe", "render")
		assertFileExists(t, filepath.Join(dir, "pdfs", "code", "report.pdf"))
	})
}

// --- uv × myst ---

func TestIntegrationUvMyst(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "uv")
	requireTool(t, "typst")
	requireTool(t, "node")

	dir := createProject(t, "uv", "myst")

	t.Run("uv sync creates virtualenv", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".venv"))
	})

	t.Run("setup registers jupyter kernel", func(t *testing.T) {
		runIn(t, dir, "uv", "run", "poe", "setup")
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "uv")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})

	t.Run("myst builds HTML site", func(t *testing.T) {
		runIn(t, dir, "uv", "run", "poe", "docs-build")
		assertFileExists(t, filepath.Join(dir, "_build", "html"))
	})

	t.Run("myst exports PDF via typst", func(t *testing.T) {
		runIn(t, dir, "uv", "run", "poe", "docs-pdf")
		assertFileExists(t, filepath.Join(dir, "pdfs", "report.pdf"))
	})
}

// --- writing (no python env, MyST → Typst PDF) ---

func TestIntegrationWriting(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "node")
	requireTool(t, "npx")
	requireTool(t, "typst")

	dir := t.TempDir()
	result, err := Create(CreateOptions{
		Name:        "test-writing",
		Dir:         dir,
		Kind:        "writing",
		AuthorName:  "Test Author",
		AuthorEmail: "test@example.com",
		Description: "Integration test manuscript",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatal("no files created")
	}

	t.Run("scaffold has no pyproject.toml", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(result.ProjectDir, "pyproject.toml")); err == nil {
			t.Error("writing project should not have pyproject.toml")
		}
	})

	t.Run("myst builds PDF via typst", func(t *testing.T) {
		runIn(t, result.ProjectDir, "npx", "mystmd", "build", "--pdf")
		assertFileExists(t, filepath.Join(result.ProjectDir, "pdfs", "main.pdf"))
	})
}

// --- pixi × none and uv × none (install-only) ---

func TestIntegrationPixiNone(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "pixi")

	dir := createProject(t, "pixi", "none")

	t.Run("pixi install creates environment", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".pixi"))
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "pixi")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})
}

func TestIntegrationUvNone(t *testing.T) {
	skipUnlessSlow(t)
	requireTool(t, "uv")

	dir := createProject(t, "uv", "none")

	t.Run("uv sync creates virtualenv", func(t *testing.T) {
		assertFileExists(t, filepath.Join(dir, ".venv"))
	})

	t.Run("marimo notebook executes and produces figure", func(t *testing.T) {
		runMarimoExport(t, dir, "uv")
		assertFileExists(t, filepath.Join(dir, "figs", "penguin_flipper_lengths.png"))
	})
}
