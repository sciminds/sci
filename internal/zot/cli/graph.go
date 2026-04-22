package cli

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/graph"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
	"github.com/urfave/cli/v3"
)

// graph-command flag destinations.
var (
	graphCitesLimit   int
	graphCitesYearMin int
)

func graphCommand() *cli.Command {
	return &cli.Command{
		Name:  "graph",
		Usage: "Traverse citation relationships between library items and OpenAlex",
		Description: "$ zot graph refs ABC12345                      # what does this paper cite?\n" +
			"$ zot graph cites ABC12345                     # what cites this paper?\n" +
			"$ zot graph cites ABC12345 --limit 50 --year-from 2022\n\n" +
			"Each result splits into in_library (Zotero keys) vs outside_library\n" +
			"(OpenAlex ids you can pipe into `item add --openalex`).",
		Commands: []*cli.Command{
			graphRefsCommand(),
			graphCitesCommand(),
		},
	}
}

func graphRefsCommand() *cli.Command {
	return &cli.Command{
		Name:        "refs",
		Usage:       "Show works this item cites, split into in-library vs outside",
		Description: "$ zot graph refs ABC12345",
		ArgsUsage:   "<key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			db, item, oa, err := graphInputs(ctx, key)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			res, err := graph.Refs(ctx, db, oa, item)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, graph.CmdResult{Result: res})
			return nil
		},
	}
}

func graphCitesCommand() *cli.Command {
	return &cli.Command{
		Name:        "cites",
		Usage:       "Show works that cite this item, split into in-library vs outside",
		Description: "$ zot graph cites ABC12345\n$ zot graph cites ABC12345 --limit 50 --year-from 2022",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max citing works to surface (1-200)", Destination: &graphCitesLimit, Local: true},
			&cli.IntFlag{Name: "year-from", Usage: "only include citing works published on or after this year", Destination: &graphCitesYearMin, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			db, item, oa, err := graphInputs(ctx, key)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			res, err := graph.Cites(ctx, db, oa, item, graph.CitesOpts{
				Limit:   graphCitesLimit,
				YearMin: graphCitesYearMin,
			})
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, graph.CmdResult{Result: res})
			return nil
		},
	}
}

// graphInputs centralizes the (db, item, openalex client) trio that both
// graph commands need. Returns the local.Reader so the caller can defer
// Close. Errors out cleanly when the item isn't in the local library —
// graph traversal needs the item's OpenAlex id or DOI to anchor.
func graphInputs(ctx context.Context, key string) (local.Reader, *local.Item, *openalex.Client, error) {
	_, opened, err := openLocalDB(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	it, err := opened.Read(key)
	if err != nil {
		_ = opened.Close()
		return nil, nil, nil, err
	}
	c, err := openalexClient()
	if err != nil {
		_ = opened.Close()
		return nil, nil, nil, err
	}
	return opened, it, c, nil
}
