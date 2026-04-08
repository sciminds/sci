// Package new scaffolds new Python projects from embedded templates.
//
// It supports two package managers (uv and pixi) and three document systems
// (Quarto, MyST, or none). Templates live in the embedded templates/python/
// directory and are rendered with [text/template] based on [TemplateVars].
//
// Key entry points:
//
//   - [Create] scaffolds a full project (templates + gitkeep dirs + post-steps)
//   - [RunWizard] runs an interactive TUI form to populate [CreateOptions]
//   - [Sync] / [PlanConfig] re-render managed config files in existing projects
//   - [RenderAll] / [RenderFile] render templates without orchestration
package new

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/ui"
)

// ---------------------------------------------------------------------------
// Options & result
// ---------------------------------------------------------------------------

// CreateOptions configures project creation.
type CreateOptions struct {
	Name        string
	Dir         string // parent directory (default ".")
	PkgManager  string // "pixi" or "uv"
	DocSystem   string // "quarto", "myst", or "none"
	AuthorName  string
	AuthorEmail string
	Description string
	DryRun      bool
}

// CreateResult holds the output of a project creation.
// It implements cmdutil.Result via [JSON] and [Human].
type CreateResult struct {
	ProjectDir string   `json:"projectDir"`
	Files      []string `json:"files"`
	PostSteps  []string `json:"postSteps,omitempty"`
	DryRun     bool     `json:"dryRun"`
}

// JSON implements cmdutil.Result.
func (r CreateResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r CreateResult) Human() string {
	var b strings.Builder
	if r.DryRun {
		fmt.Fprintf(&b, "  %s would create %d files in %s\n", ui.SymWarn, len(r.Files), ui.TUI.Accent().Render(r.ProjectDir))
	} else {
		fmt.Fprintf(&b, "  %s Created %d files in %s\n", ui.SymOK, len(r.Files), ui.TUI.Accent().Render(r.ProjectDir))
	}
	for _, f := range r.Files {
		fmt.Fprintf(&b, "    %s\n", ui.TUI.Dim().Render(f))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create scaffolds a new Python project. It renders embedded templates into a
// new directory, creates placeholder directories with .gitkeep files, and runs
// post-creation steps (git init, pixi install / uv sync).
func Create(opts CreateOptions) (*CreateResult, error) {
	if opts.Dir == "" {
		opts.Dir = "."
	}
	if opts.AuthorName == "" {
		opts.AuthorName = gitConfigValue("user.name")
	}
	if opts.AuthorEmail == "" {
		opts.AuthorEmail = gitConfigValue("user.email")
	}

	dest := filepath.Join(opts.Dir, opts.Name)

	// Check dest doesn't exist
	if _, err := os.Stat(dest); err == nil {
		return nil, fmt.Errorf("directory %q already exists", dest)
	}

	vars := TemplateVars{
		ProjectName: opts.Name,
		PkgManager:  opts.PkgManager,
		DocSystem:   opts.DocSystem,
		AuthorName:  opts.AuthorName,
		AuthorEmail: opts.AuthorEmail,
		Description: opts.Description,
	}

	if opts.DryRun {
		files, err := DryRun(vars)
		if err != nil {
			return nil, err
		}
		return &CreateResult{
			ProjectDir: dest,
			Files:      files,
			DryRun:     true,
		}, nil
	}

	// Render templates
	files, err := RenderAll(vars, dest)
	if err != nil {
		return nil, fmt.Errorf("rendering templates: %w", err)
	}

	// Create empty dirs with .gitkeep
	emptyDirs := []string{"data/derivatives", "figs"}
	for _, d := range emptyDirs {
		dirPath := filepath.Join(dest, d)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			return nil, err
		}
		gk := filepath.Join(dirPath, ".gitkeep")
		if err := os.WriteFile(gk, nil, 0o644); err != nil {
			return nil, err
		}
	}

	// Run post-steps
	steps := DefaultPostSteps(opts.PkgManager)
	var stepLabels []string
	for _, s := range steps {
		stepLabels = append(stepLabels, s.Label)
	}
	if err := RunPostSteps(steps, dest, nil); err != nil {
		// Post-step errors are non-fatal
		fmt.Fprintf(os.Stderr, "warning: post-step failed: %v\n", err)
	}

	return &CreateResult{
		ProjectDir: dest,
		Files:      files,
		PostSteps:  stepLabels,
	}, nil
}

// DryRun returns the list of files that would be created without writing
// anything to the real destination. It renders into a temp dir and discards it.
func DryRun(vars TemplateVars) ([]string, error) {
	dest, err := os.MkdirTemp("", "sci-dryrun-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dest) }()

	files, err := RenderAll(vars, dest)
	if err != nil {
		return nil, err
	}
	return files, nil
}

// ---------------------------------------------------------------------------
// Post-creation steps
// ---------------------------------------------------------------------------

// PostStep represents a shell command to run after project creation.
type PostStep struct {
	Label           string
	Cmd             []string
	ContinueOnError bool
}

// DefaultPostSteps returns the post-creation steps for a given package manager.
// Every project gets git init; pixi projects get pixi install, uv projects get
// uv sync.
func DefaultPostSteps(pkgManager string) []PostStep {
	steps := []PostStep{
		{Label: "git init", Cmd: []string{"git", "init"}, ContinueOnError: false},
	}

	switch pkgManager {
	case "pixi":
		steps = append(steps, PostStep{
			Label:           "pixi install",
			Cmd:             []string{"pixi", "install"},
			ContinueOnError: true,
		})
	case "uv":
		steps = append(steps, PostStep{
			Label:           "uv sync",
			Cmd:             []string{"uv", "sync"},
			ContinueOnError: true,
		})
	}

	return steps
}

// RunPostSteps executes steps sequentially in the given directory.
// onUpdate is called with each step label before execution (may be nil).
func RunPostSteps(steps []PostStep, dir string, onUpdate func(label string)) error {
	for _, step := range steps {
		if onUpdate != nil {
			onUpdate(step.Label)
		}

		cmd := exec.Command(step.Cmd[0], step.Cmd[1:]...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Errorf("%s: %w\n%s", step.Label, err, output)
			if !step.ContinueOnError {
				return msg
			}
			fmt.Printf("warning: %v\n", msg)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// gitConfigValue reads a single git config --global value.
func gitConfigValue(key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "--global", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
