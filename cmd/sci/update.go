package main

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/selfupdate"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

// Overridable indirections for testing — the real implementations hit the
// network and replace the process, neither of which we want in unit tests.
var (
	selfupdateCheck  = selfupdate.Check
	selfupdateUpdate = selfupdate.Update

	// execAfterUpdate replaces the running process with the freshly installed
	// sci binary, running its doctor flow.
	execAfterUpdate = func(execPath string) error {
		return syscall.Exec(execPath, postUpdateArgs(execPath), postUpdateEnv())
	}
)

// postUpdateArgs is the argv passed to the freshly installed binary.
//
// Intentionally minimal — only argv that every released sci binary will
// recognize. Newer CLI flags are unsafe here because the downloaded binary
// may predate them, and an unknown flag would error out and "bork" the
// update. The skip-upgrade-check signal is delivered via [postUpdateEnv]
// instead, which is silently ignored by older binaries.
func postUpdateArgs(execPath string) []string {
	return []string{execPath, "doctor"}
}

// postUpdateEnv returns the environment for the chained doctor process,
// with SCI_SKIP_UPGRADE_CHECK=1 appended so the new binary suppresses
// the brew/uv outdated prompt.
func postUpdateEnv() []string {
	return append(os.Environ(), "SCI_SKIP_UPGRADE_CHECK=1")
}

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:        "update",
		Usage:       "Update sci to the latest version",
		Description: "$ sci update",
		Category:    "Maintenance",
		Action:      runUpdate,
	}
}

func runUpdate(_ context.Context, cmd *cli.Command) error {
	var result selfupdate.CheckResult

	err := uikit.RunWithSpinner("Checking for updates…", func() error {
		result = selfupdateCheck()
		if result.Error != "" {
			return fmt.Errorf("%s", result.Error)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if cmdutil.IsJSON(cmd) {
		cmdutil.Output(cmd, updateResult{inner: result})
		return nil
	}

	current := selfupdate.ShortSHA(result.CurrentSHA)
	latest := selfupdate.ShortSHA(result.LatestSHA)

	if !result.Available {
		uikit.OK(fmt.Sprintf("sci is up to date (%s)", current))
		return nil
	}

	if result.DownloadURL == "" {
		return fmt.Errorf("update available but no download URL found")
	}

	fmt.Printf("  %s New version available: %s → %s\n", uikit.SymArrow, current, uikit.TUI.TextBlue().Render(latest))

	var execPath string
	err = uikit.RunWithSpinner("Downloading…", func() error {
		path, uerr := selfupdateUpdate(result.DownloadURL)
		execPath = path
		return uerr
	})
	if err != nil {
		return err
	}

	uikit.OK(fmt.Sprintf("Updated to %s", latest))

	// Chain into the freshly installed binary's doctor so any new required
	// tools (e.g. git-xet, hf for the HF cloud backend) get installed
	// without the user having to know they exist. The new binary's
	// embedded Brewfile is the authoritative source of "what sci needs"
	// — that's why we re-exec rather than calling doctor in-process.
	//
	// The binary swap above is atomic and already complete. From here on,
	// any failure in the post-update chain is a warning, never an error —
	// returning non-zero would make scripts treat the update itself as
	// failed when it actually succeeded.
	fmt.Fprintf(os.Stderr, "\n  %s Checking required tools…\n", uikit.SymArrow)
	if execErr := execAfterUpdate(execPath); execErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n", uikit.SymWarn,
			uikit.TUI.Warn().Render("Post-update setup could not start: "+execErr.Error()))
		fmt.Fprintf(os.Stderr, "  %s Run: sci doctor\n", uikit.SymArrow)
	}
	return nil
}

// updateResult wraps CheckResult to satisfy cmdutil.Result.
type updateResult struct {
	inner selfupdate.CheckResult
}

func (r updateResult) JSON() any     { return r.inner }
func (r updateResult) Human() string { return "" }
