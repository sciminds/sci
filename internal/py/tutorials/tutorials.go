// Package tutorials downloads and manages interactive tutorial notebooks
// from the SciMinds shared dataset collection.
//
// Tutorials are marimo Python notebooks hosted as public datasets. Users can
// browse available tutorials with [RunSelect] (interactive picker), download
// specific ones with [Fetch], or get everything with [FetchAll]. The
// [FetchWithAssets] variants also download companion data files and figures.
package tutorials

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/ui"
)

// Tutorial describes a single tutorial notebook available for download.
type Tutorial struct {
	Name  string // public_datasets name (e.g. "01-python-fundamentals")
	Title string // human-readable title
	File  string // local filename (e.g. "01-python-fundamentals.py")
}

// Manifest is the ordered list of available tutorials.
// Names match the public_datasets entries (derived from filename stems).
var Manifest = []Tutorial{
	{"01-python-fundamentals", "Python Fundamentals", "01-python-fundamentals.py"},
	{"02-polars-crash-course", "Polars Crash Course", "02-polars-crash-course.py"},
	{"03-seaborn", "Seaborn", "03-seaborn.py"},
	{"04-eda-workflows", "EDA Workflows", "04-eda-workflows.py"},
	{"05-sampling", "Sampling", "05-sampling.py"},
	{"06-explanation-prediction", "Explanation vs Prediction", "06-explanation-prediction.py"},
	{"07-glm-basics", "GLM Basics", "07-glm-basics.py"},
	{"08-categorical-coding", "Categorical Coding", "08-categorical-coding.py"},
	{"09-parameter-inference", "Parameter Inference", "09-parameter-inference.py"},
	{"10-marginal-effects", "Marginal Effects", "10-marginal-effects.py"},
	{"11-logistic-regression", "Logistic Regression", "11-logistic-regression.py"},
	{"12-linear-mixed-models", "Linear Mixed Models", "12-linear-mixed-models.py"},
	{"13-odds-and-ends", "Odds and Ends", "13-odds-and-ends.py"},
	{"helpers", "Helpers", "helpers.py"},
}

// Support datasets bundled as zips.
const (
	DatasetData = "tutorial-data" // zip: data/tutorials/*
	DatasetFigs = "tutorial-figs" // zip: figs/tutorials/*
)

// Packages installed into the ephemeral tutorials environment.
var Packages = []string{
	"marimo",
	"numpy",
	"scipy",
	"seaborn",
	"polars",
	"scikit-learn",
	"bossanova",
}

// pathRewriter strips ../../ (and .../../, ../../../) prefixes before
// data/tutorials/ and figs/tutorials/ so paths resolve from the flat temp dir.
var pathRewriter = regexp.MustCompile(`\.?(?:\.\./)+((data|figs)/tutorials/)`)

// RewritePaths rewrites relative path prefixes in notebook content so that
// data/tutorials/ and figs/tutorials/ resolve from the working directory.
func RewritePaths(content string) string {
	return pathRewriter.ReplaceAllString(content, "$1")
}

// Fetch downloads specific tutorials by name to destDir.
// Downloaded .py files have their asset paths rewritten.
func Fetch(names []string, destDir string) error {
	for _, name := range names {
		t := findByName(name)
		if t == nil {
			return fmt.Errorf("unknown tutorial %q", name)
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.SymArrow, t.Title)
		outPath, err := share.GetTo(t.Name, destDir)
		if err != nil {
			return fmt.Errorf("download %s: %w", t.Name, err)
		}
		if filepath.Ext(outPath) == ".py" {
			if err := rewriteFile(outPath); err != nil {
				return fmt.Errorf("rewrite %s: %w", outPath, err)
			}
		}
	}
	return nil
}

// FetchAllWithAssets downloads all tutorials plus data/figs assets.
func FetchAllWithAssets(destDir string) error {
	if err := FetchAll(destDir); err != nil {
		return err
	}
	return FetchAssets(destDir)
}

// FetchWithAssets downloads tutorials by name plus helpers and data/figs assets.
// It ensures helpers.py, data/tutorials/, and figs/tutorials/ exist in destDir.
func FetchWithAssets(names []string, destDir string) error {
	// Deduplicate and ensure helpers is included.
	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	if !seen["helpers"] {
		names = append(names, "helpers")
	}

	if err := Fetch(names, destDir); err != nil {
		return err
	}
	return FetchAssets(destDir)
}

// FetchAll downloads all tutorials to destDir.
func FetchAll(destDir string) error {
	names := make([]string, len(Manifest))
	for i, t := range Manifest {
		names[i] = t.Name
	}
	return Fetch(names, destDir)
}

// FetchAssets downloads and extracts the tutorial data and figs zips into destDir.
func FetchAssets(destDir string) error {
	for _, asset := range []string{DatasetData, DatasetFigs} {
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.SymArrow, asset)
		zipPath, err := share.GetTo(asset, destDir)
		if err != nil {
			return fmt.Errorf("download %s: %w", asset, err)
		}
		if err := extractZip(zipPath, destDir); err != nil {
			return fmt.Errorf("extract %s: %w", asset, err)
		}
		_ = os.Remove(zipPath)
	}
	return nil
}

// RunSelect launches a Bubble Tea multi-select for choosing tutorials.
// Returns the selected tutorial names.
func RunSelect() ([]string, error) {
	var filtered []huh.Option[string]
	for _, t := range Manifest {
		if t.Name == "helpers" {
			continue
		}
		label := fmt.Sprintf("%s  %s", t.File, ui.TUI.Dim().Render(t.Title))
		filtered = append(filtered, huh.NewOption(label, t.Name))
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select tutorials to download").
				Options(filtered...).
				Value(&selected),
		),
	).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap())

	if err := form.Run(); err != nil {
		return nil, err
	}

	// Always include helpers if any tutorials selected
	if len(selected) > 0 {
		selected = append(selected, "helpers")
	}
	return selected, nil
}

// Run bootstraps a temp project, fetches all tutorials + assets, and launches marimo.
func Run() error {
	uvBin, err := exec.LookPath("uv")
	if err != nil {
		return fmt.Errorf("uv not found — run %s to install it", ui.TUI.Accent().Render("sci doctor check"))
	}

	tmpDir, err := os.MkdirTemp("", "sci-tutorials-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ui.Header("Setting up tutorial environment...")

	initCmd := exec.Command(uvBin, "init", "--no-readme", "--name", "tutorials")
	initCmd.Dir = tmpDir
	initCmd.Stderr = os.Stderr
	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("uv init: %w", err)
	}

	addArgs := []string{"add"}
	addArgs = append(addArgs, Packages...)
	addCmd := exec.Command(uvBin, addArgs...)
	addCmd.Dir = tmpDir
	addCmd.Stderr = os.Stderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("uv add: %w", err)
	}

	ui.Header("Downloading tutorials...")
	if err := FetchAll(tmpDir); err != nil {
		return err
	}

	if err := FetchAssets(tmpDir); err != nil {
		return err
	}

	ui.Header("Launching marimo...")
	ui.Hint("(Ctrl+C to exit)")

	marimoCmd := exec.Command(uvBin, "run", "marimo", "edit", "--", ".")
	marimoCmd.Dir = tmpDir
	marimoCmd.Stdin = os.Stdin
	marimoCmd.Stdout = os.Stdout
	marimoCmd.Stderr = os.Stderr
	return marimoCmd.Run()
}

func findByName(name string) *Tutorial {
	for i, t := range Manifest {
		if t.Name == name {
			return &Manifest[i]
		}
	}
	return nil
}

// rewriteFile reads a file, rewrites asset paths, and writes it back.
func rewriteFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rewritten := RewritePaths(string(data))
	if rewritten == string(data) {
		return nil // no changes
	}
	return os.WriteFile(path, []byte(rewritten), 0o644)
}
