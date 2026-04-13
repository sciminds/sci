package doctor

// setup.go — optional tool picker via interactive list TUI.

import (
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
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
		status := lo.Ternary(t.Installed, "installed", "missing")
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
	if err := uikit.RunWithSpinner("Checking for required tools…", func() error {
		missing = missingSet(r, BrewfileOptional)
		return nil
	}); err != nil {
		return OptionalToolsResult{}, err
	}

	tools := lo.Map(entries, func(e brew.BrewfileEntry, _ int) OptionalToolInfo {
		return OptionalToolInfo{
			Name:      e.Name,
			Type:      e.Type,
			Installed: !missing[e.Name],
		}
	})
	return OptionalToolsResult{Tools: tools}, nil
}

// InstallOptionalTool installs a named optional tool without interactive
// prompts. Returns an error if the tool is not in the optional list or is
// already installed.
func InstallOptionalTool(r brew.Runner, name string) (OptionalSetupResult, error) {
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	entry, ok := lo.Find(entries, func(e brew.BrewfileEntry) bool {
		return e.Name == name
	})
	if !ok {
		available := lo.Map(entries, func(e brew.BrewfileEntry, _ int) string { return e.Name })
		return OptionalSetupResult{}, fmt.Errorf("unknown optional tool %q (available: %s)", name, strings.Join(available, ", "))
	}

	missing := missingSet(r, BrewfileOptional)
	if !missing[name] {
		return OptionalSetupResult{}, fmt.Errorf("tool %q is already installed", name)
	}

	tmpFile, err := brew.WriteTempBrewfile(entry.Line + "\n")
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("write temp brewfile: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	output, err := r.BundleInstall(tmpFile)
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("brew bundle install: %w", err)
	}
	return OptionalSetupResult{Installed: []string{name}, Output: output}, nil
}

// RunOptionalSetup presents a list of uninstalled optional tools and installs
// the user's selection via brew bundle install.
func RunOptionalSetup(r brew.Runner) (OptionalSetupResult, error) {
	entries := brew.ParseBrewfileEntries(BrewfileOptional)
	if len(entries) == 0 {
		return OptionalSetupResult{}, nil
	}

	// Detect which optional tools are already installed (behind a spinner).
	var missing map[string]bool
	if err := uikit.RunWithSpinner("Checking for required tools…", func() error {
		missing = missingSet(r, BrewfileOptional)
		return nil
	}); err != nil {
		return OptionalSetupResult{}, err
	}

	// All tools already installed — nothing to show.
	if lo.NoneBy(entries, func(e brew.BrewfileEntry) bool { return missing[e.Name] }) {
		return OptionalSetupResult{}, nil
	}

	// Launch list TUI — only uninstalled tools are shown.
	model, err := uikit.RunModel(newReccsModel(entries, missing))
	if err != nil {
		return OptionalSetupResult{}, err
	}
	if model.quitting || model.chosen < 0 {
		return OptionalSetupResult{}, nil
	}

	chosen := model.entries[model.chosen]

	// Build a temp Brewfile with just the selected entry.
	tmpFile, err := brew.WriteTempBrewfile(chosen.Line + "\n")
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("write temp brewfile: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	output, err := r.BundleInstall(tmpFile)
	if err != nil {
		return OptionalSetupResult{}, fmt.Errorf("brew bundle install: %w", err)
	}

	return OptionalSetupResult{Installed: []string{chosen.Name}, Output: output}, nil
}

// missingSet runs BundleCheck against the given Brewfile content and returns
// a set of package names that are not installed. On error, returns a set
// containing ALL package names from the content (assumes everything is
// missing) so callers don't incorrectly treat tools as installed.
func missingSet(r brew.Runner, content string) map[string]bool {
	tmpFile, err := brew.WriteTempBrewfile(content)
	if err != nil {
		return allNamesSet(content)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	names, err := r.BundleCheck(tmpFile)
	if err != nil {
		return allNamesSet(content)
	}
	return lo.SliceToMap(names, func(n string) (string, bool) {
		return n, true
	})
}

// allNamesSet returns a set with every package name from a Brewfile marked
// as missing. Used as a safe fallback when BundleCheck fails.
func allNamesSet(content string) map[string]bool {
	all := brew.ParseBrewfileNames(content)
	return lo.SliceToMap(all, func(n string) (string, bool) {
		return n, true
	})
}
