// Package main is the entry point for the zot standalone binary,
// a CLI for Zotero library management. Reads come from a local zotero.sqlite
// opened in immutable mode; writes go to the Zotero Web API.
//
// The command tree is shared with `sci zot` via
// github.com/sciminds/cli/internal/zot/cli.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/version"
	zotcli "github.com/sciminds/cli/internal/zot/cli"
	"github.com/urfave/cli/v3"
)

var jsonOutput bool

func main() {
	root := &cli.Command{
		Name:    "zot",
		Usage:   "Zotero library management (reads local SQLite, writes via Web API)",
		Version: version.Commit,
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
		HideVersion:     true,
		Commands:        zotcli.Commands(),
	}
	ui.SetupHelp(root)

	if err := root.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s\n", ui.SymFail, err)
		os.Exit(1)
	}
}
