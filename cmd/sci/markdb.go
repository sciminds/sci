package main

import (
	"github.com/sciminds/cli/internal/markdb"
	"github.com/urfave/cli/v3"
)

func markdbCommand() *cli.Command {
	cmd := markdb.BuildCommand(&jsonOutput)
	cmd.Name = "markdb"
	cmd.Usage = "Ingest markdown files into SQLite"
	cmd.Description = "$ sci markdb ingest ~/notes -o notes.db\n$ sci markdb search --db notes.db \"query\""
	cmd.Category = "Experimental"
	// Remove the --json flag from the subcommand; it's already on the root.
	cmd.Flags = filterFlags(cmd.Flags, "json")
	return cmd
}

func filterFlags(flags []cli.Flag, exclude string) []cli.Flag {
	out := make([]cli.Flag, 0, len(flags))
	for _, f := range flags {
		if f.Names()[0] != exclude {
			out = append(out, f)
		}
	}
	return out
}
