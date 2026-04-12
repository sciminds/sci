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
	"github.com/sciminds/cli/internal/selfupdate"
	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/version"
	"github.com/urfave/cli/v3"
)

var jsonOutput bool

func buildRoot() *cli.Command {
	// Update check: Before reads the *previous* cached result (instant) and
	// kicks off a fire-and-forget goroutine to refresh the cache for next time.
	var updateNotice string

	root := &cli.Command{
		Name:    "sci",
		Usage:   "Your scientific computing toolkit",
		Version: version.Commit,
		Flags: []cli.Flag{
			cmdutil.JSONFlag(&jsonOutput),
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			ui.SetQuiet(cmdutil.IsJSON(cmd))
			updateNotice = selfupdate.ReadCachedNotice()
			go selfupdate.RefreshCache()
			return ctx, nil
		},
		After: func(_ context.Context, cmd *cli.Command) error {
			if cmdutil.IsJSON(cmd) || (len(os.Args) > 1 && os.Args[1] == "update") {
				return nil
			}
			if updateNotice != "" {
				fmt.Fprintf(os.Stderr, "\n  %s %s\n", ui.SymArrow, updateNotice)
			}
			return nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return cli.ShowAppHelp(cmd)
		},
		Suggest:         true,
		HideHelpCommand: true,
		HideVersion:     true,
		Commands: []*cli.Command{
			// What Can I Do?
			helpCommand(),
			// Getting Started
			learnCommand(),
			// Commands
			cloudCommand(),
			dbCommand(),
			labCommand(),
			projCommand(),
			pyCommand(),
			vidCommand(),
			viewCommand(),
			// Maintenance
			toolsCommand(),
			doctorCommand(),
			updateCommand(),
			// Experimental
			cassCommand(),
			zotCommand(),
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
