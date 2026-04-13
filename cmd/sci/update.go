package main

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/selfupdate"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/urfave/cli/v3"
)

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
		result = selfupdate.Check()
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

	err = uikit.RunWithSpinner("Downloading…", func() error {
		_, uerr := selfupdate.Update(result.DownloadURL)
		return uerr
	})
	if err != nil {
		return err
	}

	uikit.OK(fmt.Sprintf("Updated to %s", latest))
	return nil
}

// updateResult wraps CheckResult to satisfy cmdutil.Result.
type updateResult struct {
	inner selfupdate.CheckResult
}

func (r updateResult) JSON() any     { return r.inner }
func (r updateResult) Human() string { return "" }
