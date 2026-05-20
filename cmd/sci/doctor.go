package main

// doctor.go — command definition, flags, and cross-platform helpers.
// The Action body lives in doctor_darwin.go and doctor_linux.go: macOS gets
// the brew-driven setup flow; Linux gets a slim preflight + identity check
// with no install side effects.

import (
	"fmt"
	"os"
	"os/exec"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/doctor"
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
		Usage:       "Check that your system is set up correctly",
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

// printAllSet prints the "all set up" line at the end of a successful run.
// Shared by both platform Action bodies.
func printAllSet() {
	fmt.Fprintf(os.Stderr, "\n  🧠 %s\n\n", uikit.TUI.Pass().Render("You're all set up!"))
}

// applyGitIdentityFlags writes any --git-name / --git-email values straight
// to global git config. Shared by both platform Action bodies.
func applyGitIdentityFlags() error {
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
	return nil
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
