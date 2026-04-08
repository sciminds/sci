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
		Description: "$ sci doctor check\n$ sci doctor reccs",
		Category:    "Maintenance",
		Commands: []*cli.Command{
			doctorCheckCommand(),
			doctorReccsCommand(),
		},
	}
}

func doctorCheckCommand() *cli.Command {
	return &cli.Command{
		Name:        "check",
		Usage:       "Check your environment and install missing tools",
		Description: "$ sci doctor check",
		Action:      runDoctorCheck,
	}
}

func doctorReccsCommand() *cli.Command {
	return &cli.Command{
		Name:        "reccs",
		Usage:       "Pick optional tools to install",
		Description: "$ sci doctor reccs",
		Action:      runDoctorReccs,
	}
}

func runDoctorCheck(_ context.Context, cmd *cli.Command) error {
	runner := brew.BundleRunner{}
	var result doctor.DocResult

	err := ui.RunWithSpinner("Checking your computer setup…", func(_, _ func(string)) error {
		result.Sections = doctor.RunAll()
		result.Tools = doctor.RunToolChecks(runner)
		return nil
	})
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)

	if cmdutil.IsJSON(cmd) {
		if !result.AllPassed() {
			os.Exit(1)
		}
		return nil
	}

	// Collect missing tools.
	var hasMissing bool
	for _, t := range result.Tools {
		if !t.Installed {
			hasMissing = true
			break
		}
	}

	if !hasMissing {
		return nil
	}

	// Offer to install missing tools.
	fmt.Fprintln(os.Stderr)
	err = cmdutil.ConfirmYes("Install missing tools?")

	if errors.Is(err, cmdutil.ErrCancelled) {
		fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
		fmt.Fprintf(os.Stderr, "    %s brew bundle install\n", ui.SymArrow)
		fmt.Fprintln(os.Stderr)
		return nil
	}
	if err != nil {
		return nil
	}

	var output string
	spinErr := ui.RunWithSpinner("Installing…", func(_, _ func(string)) error {
		var installErr error
		output, installErr = doctor.InstallAll(runner)
		return installErr
	})

	if spinErr != nil {
		fmt.Fprintf(os.Stderr, "\n  %s %s\n",
			ui.SymFail, ui.TUI.Fail().Render("Install failed: "+spinErr.Error()))
		fmt.Fprintf(os.Stderr, "\n  To install manually:\n")
		fmt.Fprintf(os.Stderr, "    %s brew bundle install\n", ui.SymArrow)
	} else {
		msg := "All tools installed successfully."
		if strings.TrimSpace(output) == "" {
			msg = "Everything up to date."
		}
		fmt.Fprintf(os.Stderr, "\n  %s\n", ui.TUI.Pass().Render(msg))
	}
	fmt.Fprintln(os.Stderr)

	return nil
}

func runDoctorReccs(_ context.Context, cmd *cli.Command) error {
	runner := brew.BundleRunner{}

	result, err := doctor.RunOptionalSetup(runner)
	if err != nil {
		return err
	}

	cmdutil.Output(cmd, result)
	return nil
}
