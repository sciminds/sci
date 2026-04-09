// Package main is the entry point for the sci CLI, a toolkit for managing
// Python-based scientific computing projects (environments, notebooks,
// databases, sharing, and more).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/cmdutil"
	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/version"
	"github.com/urfave/cli/v3"
)

var jsonOutput bool

func buildRoot() *cli.Command {
	root := &cli.Command{
		Name:    "sci",
		Usage:   "Your scientific computing toolkit",
		Version: version.Version,
		Flags: []cli.Flag{
			cmdutil.JSONFlag(&jsonOutput),
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			ui.SetQuiet(cmdutil.IsJSON(cmd))
			return ctx, nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return cli.ShowAppHelp(cmd)
		},
		Suggest:         true,
		HideHelpCommand: true,
		Commands: []*cli.Command{
			// Getting Started
			guideCommand(),
			// Commands
			cloudCommand(),
			dbCommand(),
			labCommand(),
			projCommand(),
			pyCommand(),
			vidCommand(),
			viewCommand(),
			// Maintenance
			brewCommand(),
			doctorCommand(),
			updateCommand(),
			// Experimental
			markdbCommand(),
		},
	}
	ui.SetupHelp(root)
	return root
}

func main() {
	root := buildRoot()
	if err := root.Run(context.Background(), os.Args); err != nil {
		if errors.Is(err, dbtui.ErrInterrupted) {
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.SymFail, err)
		os.Exit(1)
	}
}
