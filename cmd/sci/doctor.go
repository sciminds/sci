package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/doctor"
	"github.com/sciminds/cli/internal/ui"
	"github.com/urfave/cli/v3"
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:        "doctor",
		Usage:       "Check that your Mac is set up correctly",
		Description: "$ sci doctor",
		Category:    "Maintenance",
		Action:      runDoctorCheck,
	}
}

func runDoctorCheck(_ context.Context, cmd *cli.Command) error {
	runner := brew.BundleRunner{}

	// ── Step 1–2: Pre-flight + Identity checks ──────────────────────────
	var result doctor.DocResult
	err := ui.RunWithSpinner("Checking your computer setup…", func(_ ui.SpinnerControls) error {
		result.Sections = doctor.RunAll()
		return nil
	})
	if err != nil {
		return err
	}

	// Print pre-flight/identity results immediately.
	cmdutil.Output(cmd, result)

	if cmdutil.IsJSON(cmd) {
		if !result.AllPassed() {
			os.Exit(1)
		}
		return nil
	}

	// Bail early if Homebrew isn't installed — remaining steps need it.
	if !hasHomebrew(result) {
		return nil
	}

	// ── Step 3a: Locate or create Brewfile ───────────────────────────────
	brewfilePath, created, err := brew.ResolveBrewfile()
	if err != nil {
		return fmt.Errorf("resolve Brewfile: %w", err)
	}

	// ── Step 3b: Reconcile Brewfile with system (brew bundle dump) ──────
	if created {
		// New file — dump system state directly into it, no prompt needed.
		err = ui.RunWithSpinner("Capturing installed packages…", func(_ ui.SpinnerControls) error {
			return runner.BundleDump(brewfilePath)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				ui.SymWarn, ui.TUI.Warn().Render("Could not capture installed packages: "+err.Error()))
		} else {
			n := len(brew.ParseBrewfileNames(mustReadFile(brewfilePath)))
			fmt.Fprintf(os.Stderr, "\n  %s Created %s (%d packages)\n",
				ui.SymOK, ui.TUI.Accent().Render(brewfilePath), n)
		}
	} else {
		fmt.Fprintf(os.Stderr, "\n  %s Found Brewfile at %s\n",
			ui.SymArrow, ui.TUI.Accent().Render(brewfilePath))

		var staleEntries []brew.BrewfileEntry
		err = ui.RunWithSpinner("Comparing Brewfile with installed packages…", func(_ ui.SpinnerControls) error {
			var dumpErr error
			staleEntries, dumpErr = brew.DumpAndDiff(runner, brewfilePath)
			return dumpErr
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				ui.SymWarn, ui.TUI.Warn().Render("Could not compare Brewfile with system: "+err.Error()))
		} else if len(staleEntries) > 0 {
			names := entryNames(staleEntries)
			fmt.Fprintf(os.Stderr, "\n  Your Brewfile is out-of-date with what's currently installed (%d packages missing from file).\n", len(staleEntries))
			fmt.Fprintf(os.Stderr, "  e.g. %s\n", ui.TUI.Dim().Render(strings.Join(truncateNames(names, 5), ", ")))

			updateErr := cmdutil.ConfirmYes("Update Brewfile? (this won't change anything on your system)")
			if updateErr == nil {
				added, appendErr := brew.AppendEntries(brewfilePath, staleEntries)
				if appendErr != nil {
					return fmt.Errorf("update Brewfile: %w", appendErr)
				}
				fmt.Fprintf(os.Stderr, "  %s Added %d packages to Brewfile\n",
					ui.SymOK, len(added))
			} else if !errors.Is(updateErr, cmdutil.ErrCancelled) {
				return updateErr
			}
		}
	}

	// ── Step 3c: Ensure required packages are declared ──────────────────
	missingEntries, err := brew.MissingEntries(brewfilePath, doctor.Brewfile)
	if err != nil {
		return fmt.Errorf("check required packages: %w", err)
	}
	if len(missingEntries) > 0 {
		names := entryNames(missingEntries)
		fmt.Fprintf(os.Stderr, "\n  sci requires: %s %s\n",
			strings.Join(names, ", "),
			ui.TUI.Dim().Render("(not in your Brewfile)"))

		addErr := cmdutil.ConfirmYes("Add them?")
		if addErr == nil {
			added, appendErr := brew.AppendEntries(brewfilePath, missingEntries)
			if appendErr != nil {
				return fmt.Errorf("add required packages: %w", appendErr)
			}
			fmt.Fprintf(os.Stderr, "  %s Added %s to Brewfile\n",
				ui.SymOK, strings.Join(added, ", "))
		} else if !errors.Is(addErr, cmdutil.ErrCancelled) {
			return addErr
		}
	}

	// ── Step 4: Check & install ─────────────────────────────────────────
	var toolResult doctor.DocResult
	err = ui.RunWithSpinner("Checking installed tools…", func(_ ui.SpinnerControls) error {
		toolResult.Tools = doctor.RunToolChecks(runner, brewfilePath)
		return nil
	})
	if err != nil {
		return err
	}

	// Print tool summary.
	result.Tools = toolResult.Tools
	printToolSummary(toolResult.Tools)

	// Collect missing tools.
	var missingTools []string
	for _, t := range toolResult.Tools {
		if !t.Installed {
			missingTools = append(missingTools, t.Name)
		}
	}

	if len(missingTools) == 0 {
		printAllSet()
		return nil
	}

	// List what's missing and offer to install.
	fmt.Fprintf(os.Stderr, "\n  Missing: %s\n", strings.Join(missingTools, ", "))
	fmt.Fprintln(os.Stderr)
	installErr := cmdutil.ConfirmYes("Install missing tools?")

	if errors.Is(installErr, cmdutil.ErrCancelled) {
		fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
		fmt.Fprintf(os.Stderr, "    %s sci tools install\n", ui.SymArrow)
		fmt.Fprintln(os.Stderr)
		return nil
	}
	if installErr != nil {
		return nil
	}

	var output string
	spinErr := ui.RunWithSpinner("Installing…", func(sc ui.SpinnerControls) error {
		var instErr error
		output, instErr = runner.BundleInstallLive(brewfilePath, sc.SetStatus, sc.Suspend, sc.Resume)
		return instErr
	})

	if spinErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n",
			ui.SymFail, ui.TUI.Fail().Render("Install failed: "+spinErr.Error()))
		fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
		fmt.Fprintf(os.Stderr, "    %s sci tools install\n", ui.SymArrow)
		fmt.Fprintln(os.Stderr)
	} else {
		_ = output
		printAllSet()
	}

	return nil
}

// hasHomebrew checks if the pre-flight section includes a passing Homebrew check.
func hasHomebrew(result doctor.DocResult) bool {
	for _, sec := range result.Sections {
		for _, c := range sec.Checks {
			if c.Label == "Homebrew" {
				return c.Status == doctor.StatusPass
			}
		}
	}
	return false
}

// printToolSummary prints a one-line tools summary to stderr.
func printToolSummary(tools []doctor.ToolInfo) {
	installed := 0
	for _, t := range tools {
		if t.Installed {
			installed++
		}
	}
	sym := ui.SymOK
	if installed < len(tools) {
		sym = ui.SymFail
	}
	fmt.Fprintf(os.Stderr, "\n  %s\n", ui.TUI.Bold().Render("Tools"))
	fmt.Fprintf(os.Stderr, "    %s %-20s %s\n", sym, "installed",
		ui.TUI.Dim().Render(fmt.Sprintf("%d/%d", installed, len(tools))))
}

// entryNames extracts names from a slice of BrewfileEntry.
func entryNames(entries []brew.BrewfileEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// truncateNames returns at most n names, appending "…" if truncated.
func truncateNames(names []string, n int) []string {
	if len(names) <= n {
		return names
	}
	return append(names[:n], fmt.Sprintf("and %d more", len(names)-n))
}

// mustReadFile reads a file, returning "" on error.
func mustReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func printAllSet() {
	fmt.Fprintf(os.Stderr, "\n  🧠 %s\n\n", ui.TUI.Pass().Render("You're all set up!"))
}
