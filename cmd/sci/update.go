package main

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/selfupdate"
	"github.com/sciminds/cli/internal/ui"
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

	err := ui.RunWithSpinner("Checking for updates…", func(_, _ func(string)) error {
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

	if !result.Available {
		ui.OK("sci is up to date")
		return nil
	}

	if result.DownloadURL == "" {
		return fmt.Errorf("update available but no download URL found")
	}

	fmt.Printf("  %s New version available: %s\n", ui.SymArrow, ui.TUI.Accent().Render(result.LatestVersion))

	err = ui.RunWithSpinner("Downloading…", func(_, _ func(string)) error {
		_, uerr := selfupdate.Update(result.DownloadURL)
		return uerr
	})
	if err != nil {
		return err
	}

	ui.OK("Updated successfully")
	return nil
}

// updateResult wraps CheckResult to satisfy cmdutil.Result.
type updateResult struct {
	inner selfupdate.CheckResult
}

func (r updateResult) JSON() any     { return r.inner }
func (r updateResult) Human() string { return "" }
