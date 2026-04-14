package doctor

// setup_flow.go — RunSetup performs the Brewfile sync, required-package
// injection, tool checking, and tool installation steps of `sci doctor`.
// It is separated from the CLI handler so the full flow is testable with a
// mock [brew.Runner] and temp Brewfile.

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
)

// SetupResult holds the Brewfile and tool installation outcomes.
type SetupResult struct {
	BrewfilePath    string
	BrewfileCreated bool
	PackagesAdded   []string
	AppendError     string
	Tools           []ToolInfo
	ToolCheckError  string
	ToolsInstalled  []string
	InstallError    string
	Outdated        []brew.OutdatedPackage
	Upgraded        []brew.OutdatedPackage
	UpdateError     string
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

	// ── Sync Brewfile with system state ─────────────────────────────────
	syncResult, syncErr := brew.Sync(r, brewfilePath)
	if syncErr != nil && !uikit.IsQuiet() {
		msg := "Could not sync Brewfile with system: " + syncErr.Error()
		if created {
			msg = "Could not capture installed packages: " + syncErr.Error()
		}
		fmt.Fprintf(os.Stderr, "\n  %s %s\n", uikit.SymWarn, uikit.TUI.Warn().Render(msg))
	} else if syncErr == nil && !uikit.IsQuiet() {
		if created {
			data, _ := os.ReadFile(brewfilePath)
			n := len(brew.ParseBrewfileNames(string(data)))
			fmt.Fprintf(os.Stderr, "\n  %s Created %s (%d packages)\n",
				uikit.SymOK, uikit.TUI.TextBlue().Render(brewfilePath), n)
		} else if msg := syncResult.Human(); msg != "" {
			fmt.Fprintf(os.Stderr, "  %s %s", uikit.SymOK, msg)
		}
	}

	// ── Ensure required packages are declared ───────────────────────────
	missingEntries, err := brew.MissingEntries(brewfilePath, Brewfile)
	if err == nil && len(missingEntries) > 0 {
		if !uikit.IsQuiet() {
			names := setupEntryNames(missingEntries)
			fmt.Fprintf(os.Stderr, "\n  sci requires: %s %s\n",
				strings.Join(names, ", "),
				uikit.TUI.Dim().Render("(not in your Brewfile)"))
		}

		// In quiet mode this auto-confirms (IsQuiet check inside).
		added, appendErr := brew.AppendEntries(brewfilePath, missingEntries)
		if appendErr != nil {
			result.AppendError = appendErr.Error()
			if !uikit.IsQuiet() {
				fmt.Fprintf(os.Stderr, "  %s %s\n",
					uikit.SymWarn, uikit.TUI.Warn().Render("Could not add required packages: "+appendErr.Error()))
			}
		} else {
			result.PackagesAdded = added
			if !uikit.IsQuiet() {
				fmt.Fprintf(os.Stderr, "  %s Added %s to Brewfile\n",
					uikit.SymOK, strings.Join(added, ", "))
			}
		}
	}

	// ── Check & install tools ───────────────────────────────────────────
	tools, toolErr := RunToolChecks(r)
	if toolErr != nil {
		result.ToolCheckError = toolErr.Error()
		if !uikit.IsQuiet() {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				uikit.SymWarn, uikit.TUI.Warn().Render("Could not check tools: "+toolErr.Error()))
		}
	}
	result.Tools = tools

	missingTools := lo.FilterMap(result.Tools, func(t ToolInfo, _ int) (string, bool) {
		return t.Name, !t.Installed
	})

	if len(missingTools) > 0 {
		// Read the Brewfile to determine types of missing packages.
		content, readErr := os.ReadFile(brewfilePath)
		if readErr != nil {
			result.InstallError = readErr.Error()
		} else {
			installErr := installMissing(r, string(content), missingTools)
			if installErr != nil {
				result.InstallError = installErr.Error()
			} else {
				result.ToolsInstalled = missingTools
				for i := range result.Tools {
					if !result.Tools[i].Installed {
						result.Tools[i].Installed = true
					}
				}
			}
		}
	}

	// ── Check for outdated packages ────────────────────────────────────
	if !uikit.IsQuiet() {
		fmt.Fprintf(os.Stderr, "\n  Checking for outdated packages…\n")
	}

	if err := r.Update(); err != nil {
		result.UpdateError = err.Error()
		return result
	}

	var (
		brewOutdated, uvOutdated []brew.OutdatedPackage
		brewErr, uvErr           error
		wg                       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		brewOutdated, brewErr = r.Outdated()
	}()
	go func() {
		defer wg.Done()
		uvOutdated, uvErr = r.UVOutdated()
	}()
	wg.Wait()

	if brewErr != nil {
		result.UpdateError = brewErr.Error()
		return result
	}
	if uvErr != nil {
		result.UpdateError = uvErr.Error()
		return result
	}

	result.Outdated = append(brewOutdated, uvOutdated...)
	if len(result.Outdated) == 0 {
		return result
	}

	// Show outdated packages and prompt for upgrade.
	if !uikit.IsQuiet() {
		fmt.Fprintf(os.Stderr, "\n  %d outdated package(s):\n", len(result.Outdated))
		for _, pkg := range result.Outdated {
			arrow := uikit.TUI.TextPink().Render(" → ")
			version := uikit.TUI.TextPink().Render(pkg.InstalledVersion) + arrow + pkg.CurrentVersion
			fmt.Fprintf(os.Stderr, "    %s %s\n", pkg.Name, version)
		}
		fmt.Fprintln(os.Stderr)
	}

	if err := cmdutil.ConfirmYes("Upgrade outdated packages?"); err != nil {
		return result
	}

	if len(brewOutdated) > 0 {
		if _, err := r.Upgrade(); err != nil {
			result.UpdateError = err.Error()
			return result
		}
	}
	if len(uvOutdated) > 0 {
		names := lo.Map(uvOutdated, func(pkg brew.OutdatedPackage, _ int) string {
			return pkg.Name
		})
		if _, err := r.UVUpgrade(names); err != nil {
			result.UpdateError = err.Error()
			return result
		}
	}

	result.Upgraded = result.Outdated
	return result
}

// installMissing installs the named packages from the Brewfile content,
// grouped by type: taps → formulae → casks → uv tools.
func installMissing(r brew.Runner, content string, names []string) error {
	entries := brew.ParseBrewfileEntries(content)
	nameSet := lo.SliceToMap(names, func(n string) (string, bool) { return n, true })

	groups := lo.GroupBy(
		lo.Filter(entries, func(e brew.BrewfileEntry, _ int) bool { return nameSet[e.Name] }),
		func(e brew.BrewfileEntry) string { return e.Type },
	)
	toNames := func(typ string) []string {
		return lo.Map(groups[typ], func(e brew.BrewfileEntry, _ int) string { return e.Name })
	}

	// Taps first (individually).
	for _, name := range toNames("tap") {
		if err := r.DirectInstall(name, "tap"); err != nil {
			return fmt.Errorf("tap %s: %w", name, err)
		}
	}
	if err := r.InstallFormulae(toNames("brew")); err != nil {
		return fmt.Errorf("install formulae: %w", err)
	}
	if err := r.InstallCasks(toNames("cask")); err != nil {
		return fmt.Errorf("install casks: %w", err)
	}
	if err := r.InstallUVTools(toNames("uv")); err != nil {
		return fmt.Errorf("install uv tools: %w", err)
	}
	return nil
}

func setupEntryNames(entries []brew.BrewfileEntry) []string {
	return lo.Map(entries, func(e brew.BrewfileEntry, _ int) string {
		return e.Name
	})
}
