package doctor

// setup_flow.go — RunSetup performs the Brewfile sync, required-package
// injection, tool checking, and tool installation steps of `sci doctor`.
// It is separated from the CLI handler so the full flow is testable with a
// mock [brew.Runner] and temp Brewfile.

import (
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/ui"
)

// SetupResult holds the Brewfile and tool installation outcomes.
type SetupResult struct {
	BrewfilePath    string
	BrewfileCreated bool
	PackagesAdded   []string
	Tools           []ToolInfo
	ToolsInstalled  []string
	InstallError    string
}

// RunSetup performs Brewfile sync, required-package injection, tool checking,
// and installation. Interactive prompts use the provided confirm function,
// which should auto-confirm in quiet/JSON mode (e.g. cmdutil.ConfirmYes).
//
// brewfilePath must point to an existing file. created indicates whether the
// file was newly created (dump path) vs. pre-existing (sync path).
func RunSetup(r brew.Runner, brewfilePath string, created bool) SetupResult {
	result := SetupResult{
		BrewfilePath:    brewfilePath,
		BrewfileCreated: created,
	}

	// ── Sync or dump ────────────────────────────────────────────────────
	if created {
		err := r.BundleDump(brewfilePath)
		if err != nil && !ui.IsQuiet() {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				ui.SymWarn, ui.TUI.Warn().Render("Could not capture installed packages: "+err.Error()))
		} else if err == nil && !ui.IsQuiet() {
			data, _ := os.ReadFile(brewfilePath)
			n := len(brew.ParseBrewfileNames(string(data)))
			fmt.Fprintf(os.Stderr, "\n  %s Created %s (%d packages)\n",
				ui.SymOK, ui.TUI.Accent().Render(brewfilePath), n)
		}
	} else {
		syncResult, err := brew.Sync(r, brewfilePath, noop, noop)
		if err != nil && !ui.IsQuiet() {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				ui.SymWarn, ui.TUI.Warn().Render("Could not sync Brewfile with system: "+err.Error()))
		} else if err == nil {
			if msg := syncResult.Human(); msg != "" && !ui.IsQuiet() {
				fmt.Fprintf(os.Stderr, "  %s %s", ui.SymOK, msg)
			}
		}
	}

	// ── Ensure required packages are declared ───────────────────────────
	missingEntries, err := brew.MissingEntries(brewfilePath, Brewfile)
	if err == nil && len(missingEntries) > 0 {
		if !ui.IsQuiet() {
			names := setupEntryNames(missingEntries)
			fmt.Fprintf(os.Stderr, "\n  sci requires: %s %s\n",
				strings.Join(names, ", "),
				ui.TUI.Dim().Render("(not in your Brewfile)"))
		}

		// In quiet mode this auto-confirms (IsQuiet check inside).
		added, appendErr := brew.AppendEntries(brewfilePath, missingEntries)
		if appendErr == nil {
			result.PackagesAdded = added
			if !ui.IsQuiet() {
				fmt.Fprintf(os.Stderr, "  %s Added %s to Brewfile\n",
					ui.SymOK, strings.Join(added, ", "))
			}
		}
	}

	// ── Check & install tools ───────────────────────────────────────────
	result.Tools = RunToolChecks(r, brewfilePath)

	var missingTools []string
	for _, t := range result.Tools {
		if !t.Installed {
			missingTools = append(missingTools, t.Name)
		}
	}

	if len(missingTools) == 0 {
		return result
	}

	// Install missing tools.
	output, installErr := r.BundleInstall(brewfilePath, nil, nil, nil)
	_ = output
	if installErr != nil {
		result.InstallError = installErr.Error()
	} else {
		result.ToolsInstalled = missingTools
	}

	return result
}

func noop() {}

func setupEntryNames(entries []brew.BrewfileEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}
