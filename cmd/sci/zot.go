package main

import (
	zotcli "github.com/sciminds/cli/internal/zot/cli"
	"github.com/urfave/cli/v3"
)

// zotCommand mounts the Zotero command tree under `sci zot`. The tree lives
// in internal/zot/cli because it's substantial (20+ files) and warrants its
// own package boundary and test suite.
func zotCommand() *cli.Command {
	return &cli.Command{
		Name:  "zot",
		Usage: "Manage your Zotero library (local reads, web API writes)",
		Description: "$ sci zot guide                       # task-oriented cheat sheet (search, extraction, agent workflows)\n" +
			"$ sci zot setup\n" +
			"$ sci zot --library personal item list",
		Category: "Experimental",
		Flags:    zotcli.PersistentFlags(),
		Before:   zotcli.ValidateLibraryBefore,
		Commands: zotcli.Commands(),
	}
}
