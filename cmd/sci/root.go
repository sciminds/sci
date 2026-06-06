// Package main is the entry point for the sci CLI, a toolkit for managing
// Python-based scientific computing projects (environments, notebooks,
// databases, sharing, and more).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/selfupdate"
	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/version"
	"github.com/urfave/cli/v3"
)

var jsonOutput bool

func buildRoot() *cli.Command {
	// Update notice: rendered in Before (not After) so it surfaces even for
	// subcommands that end in syscall.Exec (REPL, marimo, quarto, lab
	// get/put/connect, py, proj/exec) or sit inside an alt-screen TUI —
	// urfave/cli's After hook is unreachable from any of those paths.
	// Trade-off: the notice prints at the top of output instead of the
	// bottom. For exec/TUI flows the line stays on the main-screen
	// scrollback and is restored when the user exits.
	root := &cli.Command{
		Name:    "sci",
		Usage:   "Your scientific computing toolkit",
		Version: version.Commit,
		Flags: []cli.Flag{
			cmdutil.JSONFlag(&jsonOutput),
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			selfupdate.SpawnDetachedRefresh()
			// Suppress for --json (machine output) and for `sci update`
			// itself (the user is already running the updater). At root's
			// Before, cmd is the root command — peek at the first positional
			// arg to identify the resolved subcommand.
			if !cmdutil.IsJSON(cmd) && cmd.Args().First() != "update" {
				if notice := selfupdate.ReadCachedNotice(); notice != "" {
					fmt.Fprintf(os.Stderr, "\n  %s %s\n", uikit.SymArrow, notice)
					selfupdate.MarkNoticeShown()
				}
			}
			return ctx, nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return cli.ShowAppHelp(cmd)
		},
		Suggest:         true,
		HideHelpCommand: true,
		HideVersion:     true,
		// lo.Compact filters out any nil entries — used on Linux where
		// platform-specific commands (e.g. `sci tools`) return nil from
		// their stubbed constructor.
		Commands: lo.Compact([]*cli.Command{
			// What Can I Do?
			helpCommand(),
			// Getting Started
			learnCommand(),
			setupCommand(),
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
		}),
	}
	cmdutil.SetupHelp(root)
	cmdutil.WireNamespaceDefaults(root)
	return root
}

func main() {
	// Detached refresh trampoline: when [selfupdate.SpawnDetachedRefresh]
	// re-execs this binary with the sentinel env var set, do the (slow)
	// update check and exit. This bypasses every urfave/cli code path so
	// the child cannot accidentally execute a user-facing command.
	if os.Getenv(selfupdate.InternalRefreshEnv) == "1" {
		selfupdate.RefreshCache()
		return
	}

	root := buildRoot()
	if err := root.Run(context.Background(), os.Args); err != nil {
		if errors.Is(err, dbtui.ErrInterrupted) {
			os.Exit(130)
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", uikit.SymFail, err)
		os.Exit(1)
	}
}
