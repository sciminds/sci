package doctor

// setup.go — optional tool picker via huh multi-select form.

import (
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/ui"
)

// OptionalSetupResult reports the outcome of the optional tool install.
type OptionalSetupResult struct {
	Installed []string `json:"installed"`
	Output    string   `json:"output,omitempty"`
}

// JSON returns the structured output.
func (r OptionalSetupResult) JSON() any { return r }

// Human returns the styled terminal output.
func (r OptionalSetupResult) Human() string {
	if len(r.Installed) == 0 {
		return "No tools selected.\n"
	}
	return fmt.Sprintf("Installed %d tools: %s\n", len(r.Installed), strings.Join(r.Installed, ", "))
}

// OptionalToolInfo describes an optional tool and its install status.
type OptionalToolInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Installed bool   `json:"installed"`
}

// OptionalToolsResult reports optional tools and their install status.
type OptionalToolsResult struct {
	Tools []OptionalToolInfo `json:"tools"`
}

// JSON returns the structured output.
func (r OptionalToolsResult) JSON() any { return r }

// Human returns the styled terminal output.
func (r OptionalToolsResult) Human() string {
	if len(r.Tools) == 0 {
		return "No optional tools available.\n"
	}
	var b strings.Builder
	for _, t := range r.Tools {
		status := "missing"
		if t.Installed {
			status = "installed"
		}
		fmt.Fprintf(&b, "  %s (%s): %s\n", t.Name, t.Type, status)
	}
	return b.String()
}

// ListOptionalTools returns optional tools with their install status,
// without prompting the user. Used in --json mode.
func ListOptionalTools(r brew.Runner) (OptionalToolsResult, error) {
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	if len(entries) == 0 {
		return OptionalToolsResult{}, nil
	}

	var missing map[string]bool
	if err := ui.RunWithSpinner("Checking installed tools…", func(_ ui.SpinnerControls) error {
		missing = missingSet(r, BrewfileOptional)
		return nil
	}); err != nil {
		return OptionalToolsResult{}, err
	}

	tools := make([]OptionalToolInfo, len(entries))
	for i, e := range entries {
		tools[i] = OptionalToolInfo{
			Name:      e.Name,
			Type:      e.Type,
			Installed: !missing[e.Name],
		}
	}
	return OptionalToolsResult{Tools: tools}, nil
}

// RunOptionalSetup presents a multi-select of optional tools and installs the
// user's selections via brew bundle install.
func RunOptionalSetup(r brew.Runner) (OptionalSetupResult, error) {
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	if len(entries) == 0 {
		return OptionalSetupResult{}, nil
	}

	// Detect which optional tools are already installed (behind a spinner).
	var missing map[string]bool
	if err := ui.RunWithSpinner("Checking installed tools…", func(_ ui.SpinnerControls) error {
		missing = missingSet(r, BrewfileOptional)
		return nil
	}); err != nil {
		return OptionalSetupResult{}, err
	}

	options := make([]huh.Option[int], len(entries))
	var selected []int
	for i, e := range entries {
		label := e.Name + ui.TUI.Dim().Render(" "+e.Type)
		if !missing[e.Name] {
			label += ui.TUI.Dim().Render(" ✓")
			selected = append(selected, i)
		}
		options[i] = huh.NewOption(label, i)
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[int]().
				Title("Select optional tools to install").
				Options(options...).
				Value(&selected),
		),
	).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap())

	if err := form.Run(); err != nil {
		return OptionalSetupResult{}, err
	}

	if len(selected) == 0 {
		return OptionalSetupResult{}, nil
	}

	// Build a temp Brewfile from selected entries.
	var lines []string
	var names []string
	for _, idx := range selected {
		lines = append(lines, entries[idx].Line)
		names = append(names, entries[idx].Name)
	}

	tmpFile, err := brew.WriteTempBrewfile(strings.Join(lines, "\n") + "\n")
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("write temp brewfile: %w", err)
	}

	output, err := r.BundleInstall(tmpFile)
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("brew bundle install: %w", err)
	}

	return OptionalSetupResult{Installed: names, Output: output}, nil
}

// missingSet runs BundleCheck against the given Brewfile content and returns
// a set of package names that are not installed.
func missingSet(r brew.Runner, content string) map[string]bool {
	tmpFile, err := brew.WriteTempBrewfile(content)
	if err != nil {
		return nil
	}
	defer func() { _ = os.Remove(tmpFile) }()

	names, err := r.BundleCheck(tmpFile)
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}
