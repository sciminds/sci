package cli

import (
	"context"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/graph"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
	"github.com/urfave/cli/v3"
)

// graph-command flag destinations.
var (
	graphRefsLimit    int
	graphCitesLimit   int
	graphCitesYearMin int
	graphRefsRemote   bool
	graphCitesRemote  bool
	graphRefsVerbose  bool
	graphCitesVerbose bool
)

func graphCommand() *cli.Command {
	return &cli.Command{
		Name:  "graph",
		Usage: "Traverse citation relationships between library items and OpenAlex",
		Description: "Direction cheat-sheet (the source paper is `ABC12345`):\n" +
			"  refs  → outgoing edges — works THIS paper cites (its bibliography)\n" +
			"  cites → incoming edges — works that cite THIS paper (impact)\n\n" +
			"$ sci zot graph refs ABC12345                      # what does this paper cite?\n" +
			"$ sci zot graph cites ABC12345                     # what cites this paper?\n" +
			"$ sci zot graph cites ABC12345 --limit 50 --year-from 2022\n\n" +
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
		Name:  "refs",
		Usage: "Show works this item cites, split into in-library vs outside",
		Description: "$ sci zot graph refs ABC12345\n" +
			"$ sci zot graph refs ABC12345 --limit 50    # default 25; pass 0 for the full bibliography\n" +
			"$ sci zot graph refs ABC12345 --remote      # bypass local sqlite, hit the Zotero Web API",
		ArgsUsage: "<key>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max neighbors to surface (in_library kept first, then outside_library; 0 = unlimited). Stats keeps the original totals so truncation is visible.", Destination: &graphRefsLimit, Local: true},
			&cli.BoolFlag{Name: "remote", Usage: "fetch the source item from the Zotero Web API (use when the item was just created and isn't synced yet)", Destination: &graphRefsRemote, Local: true},
			&cli.BoolFlag{Name: "verbose", Usage: "--json: emit full author lists (default caps each neighbor at 3 authors)", Destination: &graphRefsVerbose, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			db, item, oa, err := graphInputs(ctx, key, graphRefsRemote)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			idx, err := libraryIndex(ctx, db, graphRefsRemote)
			if err != nil {
				return err
			}
			res, err := graph.Refs(ctx, idx, oa, item, graph.RefsOpts{Limit: graphRefsLimit})
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, graph.CmdResult{Result: res, Verbose: graphRefsVerbose})
			return nil
		},
	}
}

func graphCitesCommand() *cli.Command {
	return &cli.Command{
		Name:        "cites",
		Usage:       "Show works that cite this item, split into in-library vs outside",
		Description: "$ sci zot graph cites ABC12345\n$ sci zot graph cites ABC12345 --limit 50 --year-from 2022\n$ sci zot graph cites ABC12345 --remote",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max citing works to surface (1-200)", Destination: &graphCitesLimit, Local: true},
			&cli.IntFlag{Name: "year-from", Usage: "only include citing works published on or after this year", Destination: &graphCitesYearMin, Local: true},
			&cli.BoolFlag{Name: "remote", Usage: "fetch the source item from the Zotero Web API (use when the item was just created and isn't synced yet)", Destination: &graphCitesRemote, Local: true},
			&cli.BoolFlag{Name: "verbose", Usage: "--json: emit full author lists (default caps each neighbor at 3 authors)", Destination: &graphCitesVerbose, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			db, item, oa, err := graphInputs(ctx, key, graphCitesRemote)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			idx, err := libraryIndex(ctx, db, graphCitesRemote)
			if err != nil {
				return err
			}
			res, err := graph.Cites(ctx, idx, oa, item, graph.CitesOpts{
				Limit:   graphCitesLimit,
				YearMin: graphCitesYearMin,
			})
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, graph.CmdResult{Result: res, Verbose: graphCitesVerbose})
			return nil
		},
	}
}

// libraryIndex builds the graph.LibraryIndex used for in-library DOI
// intersection. With remote=false we wrap the already-open local
// reader (cheap, may be stale). With remote=true we pre-fetch every
// item in the configured library via the Zotero Web API so refs/cites
// against just-added items resolve to in_library hits without waiting
// for Zotero desktop to sync.
func libraryIndex(ctx context.Context, db local.Reader, remote bool) (graph.LibraryIndex, error) {
	if !remote {
		return graph.LocalIndex(db), nil
	}
	zc, err := requireAPIClient(ctx)
	if err != nil {
		return nil, err
	}
	return graph.RemoteIndex(ctx, zc), nil
}

// graphInputs centralizes the (db, item, openalex client) trio that both
// graph commands need. The local.Reader is always opened (used for
// LocalIndex even when --remote drives source resolution against the
// API); when remote is true the source item itself is fetched via the
// Zotero Web API instead of db.Read so just-created items work even
// before Zotero desktop syncs.
func graphInputs(ctx context.Context, key string, remote bool) (local.Reader, *local.Item, *openalex.Client, error) {
	_, opened, err := openLocalDB(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	c, err := openalexClient()
	if err != nil {
		_ = opened.Close()
		return nil, nil, nil, err
	}
	var item *local.Item
	if remote {
		zc, rerr := requireAPIClient(ctx)
		if rerr != nil {
			_ = opened.Close()
			return nil, nil, nil, rerr
		}
		raw, rerr := zc.GetItem(ctx, key)
		if rerr != nil {
			_ = opened.Close()
			return nil, nil, nil, rerr
		}
		it := api.ItemFromClient(raw)
		item = &it
	} else {
		it, rerr := opened.Read(key)
		if rerr != nil {
			_ = opened.Close()
			return nil, nil, nil, rerr
		}
		item = it
	}
	return opened, item, c, nil
}
