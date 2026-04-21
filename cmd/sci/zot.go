package main

import (
	zotcli "github.com/sciminds/cli/internal/zot/cli"
	"github.com/urfave/cli/v3"
)

// zotCommand wires the shared zot command tree into sci as a subcommand.
// Both `sci zot …` and the standalone `zot …` binary (cmd/zot) reuse the
// same command tree via internal/zot/cli.Commands().
func zotCommand() *cli.Command {
	return &cli.Command{
		Name:        "zot",
		Usage:       "Manage your Zotero library (local reads, web API writes)",
		Description: "$ sci zot setup\n$ sci zot --library personal item list",
		Category:    "Experimental",
		Flags:       zotcli.PersistentFlags(),
		Before:      zotcli.ValidateLibraryBefore,
		Commands:    zotcli.Commands(),
	}
}
