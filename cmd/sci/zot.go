package main

import (
	zotcli "github.com/sciminds/sci/internal/zot/cli"
	"github.com/urfave/cli/v3"
)

// zotCommand mounts the Zotero command tree under `sci zot`. The tree lives
// in internal/zot/cli because it's substantial (20+ files) and warrants its
// own package boundary and test suite.
func zotCommand() *cli.Command {
	return &cli.Command{
		Name:  "zot",
		Usage: "Query and cite your Zotero library, read-only",
		Description: "# agents: run `sci zot guide --json` once before driving zot — it teaches the command surface and the --json envelope/fix/warnings contract\n" +
			"$ sci zot guide                       # task-oriented cheat sheet (search, bibliographies, hygiene)\n" +
			"$ sci zot setup\n" +
			"$ sci zot --library personal search \"theory of mind\"\n" +
			"$ sci zot --library personal bib paper.qmd --out refs.bib",
		Category: "Commands",
		Flags:    zotcli.PersistentFlags(),
		Before:   zotcli.ValidateLibraryBefore,
		Commands: zotcli.Commands(),
	}
}
