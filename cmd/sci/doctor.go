package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/doctor"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

var (
	doctorGitName          string
	doctorGitEmail         string
	doctorYes              bool
	doctorSkipUpgradeCheck bool
)

func doctorCommand() *cli.Command {
	return &cli.Command{
		Name:        "doctor",
		Usage:       "Check that your Mac is set up correctly",
		Description: "$ sci doctor\n$ sci doctor --json --git-name \"Jane Doe\" --git-email jane@example.com",
		Category:    "Maintenance",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "git-name",
				Usage:       "set git user.name (skips interactive prompt)",
				Destination: &doctorGitName,
				Local:       true,
			},
			&cli.StringFlag{
				Name:        "git-email",
				Usage:       "set git user.email (skips interactive prompt)",
				Destination: &doctorGitEmail,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "yes",
				Usage:       "auto-confirm prerequisite installs (e.g. Homebrew) — required to drive a fresh-machine setup under --json",
				Destination: &doctorYes,
				Local:       true,
			},
			&cli.BoolFlag{
				Name:        "skip-upgrade-check",
				Usage:       "skip the brew/uv outdated check and upgrade prompt (used by `sci update`)",
				Destination: &doctorSkipUpgradeCheck,
				Local:       true,
			},
		},
		Action: runDoctorCheck,
	}
}

// postUpdateEnvVar is the env-var equivalent of --skip-upgrade-check, set by
// `sci update` when re-execing into the new binary. Env vars are silently
// ignored by binaries that predate this hook, so they're version-skew-safe
// across self-updates in a way that unknown CLI flags are not.
const postUpdateEnvVar = "SCI_SKIP_UPGRADE_CHECK"

// skipUpgradeCheck reports whether the upgrade-check step should be
// suppressed — either via the flag or the env var set by `sci update`.
func skipUpgradeCheck() bool {
	return doctorSkipUpgradeCheck || os.Getenv(postUpdateEnvVar) == "1"
}

func runDoctorCheck(_ context.Context, cmd *cli.Command) error {
	runner := brew.BrewRunner{}
	isJSON := cmdutil.IsJSON(cmd)

	// ── Step 0: Apply git identity flags ────────────────────────────────
	if doctorGitName != "" {
		if err := exec.Command("git", "config", "--global", "user.name", doctorGitName).Run(); err != nil {
			return fmt.Errorf("set git user.name: %w", err)
		}
	}
	if doctorGitEmail != "" {
		if err := exec.Command("git", "config", "--global", "user.email", doctorGitEmail).Run(); err != nil {
			return fmt.Errorf("set git user.email: %w", err)
		}
	}

	// ── Step 1–2: Pre-flight + Identity checks ──────────────────────────
	var result doctor.DocResult
	result.Sections = doctor.RunPreflightIdentity()

	// In human mode, print checks immediately so the user sees progress.
	if !isJSON {
		cmdutil.Output(cmd, result)
	}

	// Prompt for missing git identity (interactive mode only, when flags weren't used).
	if !isJSON {
		if err := promptGitIdentity(result); err != nil {
			return err
		}
	}

	// Remaining steps need network access (Homebrew sync, install, updates).
	if !netutil.Online() {
		if !isJSON {
			fmt.Fprintf(os.Stderr, "\n  %s %s\n",
				uikit.SymWarn, uikit.TUI.Warn().Render("No internet connection — skipping Homebrew checks"))
		}
		if isJSON {
			cmdutil.Output(cmd, result)
		}
		return nil
	}

	// Bail early if Homebrew isn't installed — remaining steps need it.
	if !hasHomebrew(result) {
		if isJSON && !doctorYes {
			// JSON without --yes: report state and exit. Installing
			// Homebrew is a side effect we don't take silently.
			cmdutil.Output(cmd, result)
			if !result.AllPassed() {
				os.Exit(1)
			}
			return nil
		}

		// Interactive (any mode): offer to install with confirmation.
		// JSON + --yes: install non-interactively. Both paths re-run
		// pre-flight on success so the rest of doctor can proceed.
		var installed bool
		if isJSON {
			installed = installHomebrewQuiet()
		} else {
			installed = offerHomebrewInstall()
		}
		if !installed {
			if isJSON {
				cmdutil.Output(cmd, result)
				os.Exit(1)
			}
			return nil
		}
		result.Sections = doctor.RunPreflightIdentity()
		if !hasHomebrew(result) {
			if isJSON {
				cmdutil.Output(cmd, result)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "\n  %s %s\n", uikit.SymWarn,
				uikit.TUI.Warn().Render(`brew not on PATH yet — run: eval "$(/opt/homebrew/bin/brew shellenv)"`))
			fmt.Fprintf(os.Stderr, "  %s then re-run: sci doctor\n", uikit.SymArrow)
			return nil
		}
	}

	// ── Step 3a: Locate or create Brewfile ───────────────────────────────
	brewfilePath, created, err := brew.ResolveBrewfile()
	if err != nil {
		return fmt.Errorf("resolve Brewfile: %w", err)
	}

	if !isJSON && !created {
		fmt.Fprintf(os.Stderr, "\n  %s Found Brewfile at %s\n",
			uikit.SymArrow, uikit.TUI.TextBlue().Render(brewfilePath))
	}

	// ── Steps 3b–4: Sync, required packages, tool check & install ──────
	if isJSON {
		// Non-interactive: RunSetup handles everything, auto-confirms.
		setup := doctor.RunSetup(runner, brewfilePath, created, doctor.SetupOpts{
			SkipUpgradeCheck: skipUpgradeCheck(),
		})
		result.BrewfilePath = setup.BrewfilePath
		result.BrewfileCreated = setup.BrewfileCreated
		result.PackagesAdded = setup.PackagesAdded
		result.AppendError = setup.AppendError
		result.Tools = setup.Tools
		result.ToolCheckError = setup.ToolCheckError
		result.ToolsInstalled = setup.ToolsInstalled
		result.InstallError = setup.InstallError
		result.Outdated = setup.Outdated
		result.Upgraded = setup.Upgraded
		result.UpdateError = setup.UpdateError

		// Re-run preflight: tools the bundle just installed (gh, uv, etc.)
		// weren't on PATH when the initial preflight ran, so checks like
		// "GitHub CLI auth" would otherwise stay stale and false-fail
		// AllPassed in JSON mode.
		result.Sections = doctor.RunPreflightIdentity()

		cmdutil.Output(cmd, result)
		if !result.AllPassed() || result.InstallError != "" {
			os.Exit(1)
		}
		return nil
	}

	// ── Interactive path (human mode) ───────────────────────────────────
	// The initial Sync used to run here, but on a fresh machine uv isn't
	// installed yet — Sync's `uv tool list` would fail. Install required
	// tools first (steps 3c → 4), then reconcile via Sync just before the
	// outdated check.

	if created {
		n := len(brew.ParseBrewfileNames(mustReadFile(brewfilePath)))
		fmt.Fprintf(os.Stderr, "\n  %s Created %s (%d packages)\n",
			uikit.SymOK, uikit.TUI.TextBlue().Render(brewfilePath), n)
	}

	// Step 3c: Ensure required packages are declared.
	missingEntries, err := brew.MissingEntries(brewfilePath, doctor.Brewfile)
	if err != nil {
		return fmt.Errorf("check required packages: %w", err)
	}
	if len(missingEntries) > 0 {
		names := entryNames(missingEntries)
		fmt.Fprintf(os.Stderr, "\n  sci requires: %s %s\n",
			strings.Join(names, ", "),
			uikit.TUI.Dim().Render("(not in your Brewfile)"))

		addErr := cmdutil.ConfirmYes("Add them?")
		if addErr == nil {
			added, appendErr := brew.AppendEntries(brewfilePath, missingEntries)
			if appendErr != nil {
				return fmt.Errorf("add required packages: %w", appendErr)
			}
			fmt.Fprintf(os.Stderr, "  %s Added %s to Brewfile\n",
				uikit.SymOK, strings.Join(added, ", "))
		} else if !errors.Is(addErr, cmdutil.ErrCancelled) {
			return addErr
		}
	}

	// Step 4: Check & install.
	var toolInfos []doctor.ToolInfo
	var toolCheckErr error
	err = uikit.RunWithSpinner("Checking for required tools…", func() error {
		toolInfos, toolCheckErr = doctor.RunToolChecks(runner)
		return nil
	})
	if err != nil {
		return err
	}

	if toolCheckErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n",
			uikit.SymWarn, uikit.TUI.Warn().Render("Could not check tools: "+toolCheckErr.Error()))
		// Can't install if we don't know what's missing — skip to update check.
		if skipUpgradeCheck() {
			return nil
		}
		return runDoctorUpdateCheck(runner)
	}

	result.Tools = toolInfos
	printToolSummary(toolInfos)

	missingTools := lo.FilterMap(toolInfos, func(t doctor.ToolInfo, _ int) (string, bool) {
		return t.Name, !t.Installed
	})

	if len(missingTools) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Missing: %s\n", strings.Join(missingTools, ", "))
		fmt.Fprintln(os.Stderr)
		installErr := cmdutil.ConfirmYes("Install missing tools?")

		if errors.Is(installErr, cmdutil.ErrCancelled) {
			fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
			fmt.Fprintf(os.Stderr, "    %s sci tools install\n", uikit.SymArrow)
			fmt.Fprintln(os.Stderr)
		} else if installErr != nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "  Installing…\n")
			_, spinErr := brew.Install(runner, brewfilePath)
			if spinErr != nil {
				fmt.Fprintf(os.Stderr, "\n  %s %s\n",
					uikit.SymFail, uikit.TUI.Fail().Render("Install failed: "+spinErr.Error()))
				fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
				fmt.Fprintf(os.Stderr, "    %s sci tools install\n", uikit.SymArrow)
				fmt.Fprintln(os.Stderr)
			}
		}
	}

	// Reconcile the Brewfile with the final system state. Non-fatal.
	if syncResult, syncErr := brew.Sync(runner, brewfilePath); syncErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n",
			uikit.SymWarn, uikit.TUI.Warn().Render("Could not sync Brewfile: "+syncErr.Error()))
	} else if !created {
		if msg := syncResult.Human(); msg != "" {
			fmt.Fprintf(os.Stderr, "  %s %s", uikit.SymOK, msg)
		}
	}

	// ── Step 5: Check for outdated packages ─────────────────────────────
	if skipUpgradeCheck() {
		printAllSet()
		return nil
	}
	return runDoctorUpdateCheck(runner)
}

// offerHomebrewInstall prompts the user to install Homebrew and runs the
// official installer on accept. Returns true if brew appears to be installed
// after this call (either the install succeeded, or it was already present).
func offerHomebrewInstall() bool {
	fmt.Fprintf(os.Stderr, "\n  %s Homebrew is required for sci to manage your tools.\n", uikit.SymArrow)
	err := cmdutil.ConfirmRequired("Install Homebrew now?")
	if errors.Is(err, cmdutil.ErrCancelled) {
		fmt.Fprintf(os.Stderr, "\n  To install manually: visit https://brew.sh, then re-run sci doctor.\n\n")
		return false
	}
	if err != nil {
		return false
	}

	fmt.Fprintf(os.Stderr, "\n  Installing Homebrew (this may take a few minutes)…\n\n")
	installErr := brew.InstallHomebrew()
	if errors.Is(installErr, brew.ErrHomebrewInstalled) {
		return true
	}
	if installErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n", uikit.SymFail,
			uikit.TUI.Fail().Render("Homebrew install failed: "+installErr.Error()))
		fmt.Fprintf(os.Stderr, "  %s Visit https://brew.sh to install manually.\n\n", uikit.SymArrow)
		return false
	}
	fmt.Fprintf(os.Stderr, "\n  %s Homebrew installed\n", uikit.SymOK)
	return true
}

// installHomebrewQuiet is the JSON/--yes counterpart to offerHomebrewInstall:
// runs the official installer without prompting and returns whether brew
// is now present.
func installHomebrewQuiet() bool {
	installErr := brew.InstallHomebrew()
	if errors.Is(installErr, brew.ErrHomebrewInstalled) {
		return true
	}
	if installErr != nil {
		fmt.Fprintf(os.Stderr, "Homebrew install failed: %s\n", installErr.Error())
		return false
	}
	return true
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
	installed := lo.CountBy(tools, func(t doctor.ToolInfo) bool {
		return t.Installed
	})
	sym := uikit.SymOK
	if installed < len(tools) {
		sym = uikit.SymFail
	}
	fmt.Fprintf(os.Stderr, "\n  %s\n", uikit.TUI.Bold().Render("Tools"))
	fmt.Fprintf(os.Stderr, "    %s %-20s %s\n", sym, "installed",
		uikit.TUI.Dim().Render(fmt.Sprintf("%d/%d", installed, len(tools))))
}

// entryNames extracts names from a slice of BrewfileEntry.
func entryNames(entries []brew.BrewfileEntry) []string {
	return lo.Map(entries, func(e brew.BrewfileEntry, _ int) string {
		return e.Name
	})
}

// mustReadFile reads a file, returning "" on error.
func mustReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// runDoctorUpdateCheck refreshes the registry, checks for outdated packages,
// and offers to upgrade them — the interactive equivalent of
// `sci tools outdated && sci tools update`.
func runDoctorUpdateCheck(runner brew.Runner) error {
	fmt.Fprintf(os.Stderr, "\n  Checking for outdated packages…\n")

	result, err := brew.Update(runner, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n",
			uikit.SymWarn, uikit.TUI.Warn().Render("Could not check for updates: "+err.Error()))
		return nil
	}

	if len(result.Outdated) == 0 {
		printAllSet()
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n  %d outdated package(s):\n", len(result.Outdated))
	for _, pkg := range result.Outdated {
		arrow := uikit.TUI.TextPink().Render(" → ")
		version := uikit.TUI.TextPink().Render(pkg.InstalledVersion) + arrow + pkg.CurrentVersion
		fmt.Fprintf(os.Stderr, "    %s %s\n", pkg.Name, version)
	}
	fmt.Fprintln(os.Stderr)

	upgradeErr := cmdutil.ConfirmYes("Upgrade outdated packages?")
	if errors.Is(upgradeErr, cmdutil.ErrCancelled) {
		fmt.Fprintf(os.Stderr, "\n  To upgrade manually:\n")
		fmt.Fprintf(os.Stderr, "    %s sci tools update\n", uikit.SymArrow)
		fmt.Fprintln(os.Stderr)
		return nil
	}
	if upgradeErr != nil {
		return nil
	}

	fmt.Fprintf(os.Stderr, "  Upgrading…\n")
	_, err = brew.UpgradeOnly(runner)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n",
			uikit.SymFail, uikit.TUI.Fail().Render("Upgrade failed: "+err.Error()))
		fmt.Fprintf(os.Stderr, "\n  To upgrade manually:\n")
		fmt.Fprintf(os.Stderr, "    %s sci tools update\n", uikit.SymArrow)
		fmt.Fprintln(os.Stderr)
		return nil
	}

	printAllSet()
	return nil
}

func printAllSet() {
	fmt.Fprintf(os.Stderr, "\n  🧠 %s\n\n", uikit.TUI.Pass().Render("You're all set up!"))
}

// promptGitIdentity checks whether git user.name or user.email are missing
// (and weren't supplied via flags) and prompts the user to set them.
func promptGitIdentity(result doctor.DocResult) error {
	needName := doctorGitName == "" && gitIdentityMissing(result, "Git user.name")
	needEmail := doctorGitEmail == "" && gitIdentityMissing(result, "Git user.email")
	if !needName && !needEmail {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n")

	var name, email string
	var fields []huh.Field
	if needName {
		fields = append(fields, huh.NewInput().
			Title("Git user.name").
			Description("Used in your git commits (e.g. Jane Doe)").
			Value(&name))
	}
	if needEmail {
		fields = append(fields, huh.NewInput().
			Title("Git user.email").
			Description("Used in your git commits (e.g. jane@example.com)").
			Value(&email))
	}

	if err := uikit.RunForm(huh.NewForm(huh.NewGroup(fields...))); err != nil {
		return err
	}

	if name != "" {
		if err := exec.Command("git", "config", "--global", "user.name", name).Run(); err != nil {
			return fmt.Errorf("set git user.name: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Set git user.name to %s\n", uikit.SymOK, uikit.TUI.TextBlue().Render(name))
	}
	if email != "" {
		if err := exec.Command("git", "config", "--global", "user.email", email).Run(); err != nil {
			return fmt.Errorf("set git user.email: %w", err)
		}
		fmt.Fprintf(os.Stderr, "  %s Set git user.email to %s\n", uikit.SymOK, uikit.TUI.TextBlue().Render(email))
	}

	return nil
}

// gitIdentityMissing returns true if the named check (e.g. "Git user.name")
// has a failing status in the doctor results.
func gitIdentityMissing(result doctor.DocResult, label string) bool {
	for _, sec := range result.Sections {
		for _, c := range sec.Checks {
			if c.Label == label {
				return c.Status == doctor.StatusFail
			}
		}
	}
	return false
}
