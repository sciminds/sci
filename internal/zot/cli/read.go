package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/ui"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// Read-command flag destinations.
var (
	listType       string
	listCollection string
	listTag        string
	listLimit      int
	listOffset     int
	listOrder      string

	searchLimit int

	exportFormat string
	exportOut    string
)

// openLocalDB loads config, opens the local zotero.sqlite, and warns if the
// schema version is outside the tested range.
func openLocalDB() (*zot.Config, *local.DB, error) {
	cfg, err := zot.RequireConfig()
	if err != nil {
		return nil, nil, err
	}
	db, err := local.Open(cfg.DataDir)
	if err != nil {
		return nil, nil, err
	}
	if db.SchemaOutOfRange() {
		fmt.Fprintf(os.Stderr, "  %s Zotero schema version %d is outside the tested range [%d, %d] — proceeding anyway\n",
			ui.SymArrow, db.SchemaVersion(), local.MinTestedSchemaVersion, local.MaxTestedSchemaVersion)
	}
	return cfg, db, nil
}

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:  "search",
		Usage: "Search your library by title, DOI, or publication",
		Description: "$ zot search \"large language models\"\n" +
			"$ zot search --limit 100 neuroimaging\n" +
			"$ zot search attention --export --out hits.bib",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max results", Destination: &searchLimit, Local: true},
			// --export routes the hit list through the same pipeline as
			// `zot export`. Bool flag — always emits bibtex, which is
			// the format this feature exists to serve. Users who want
			// CSL-JSON should use the top-level `zot export` command.
			&cli.BoolFlag{Name: "export", Usage: "emit results as bibtex instead of the normal hit list", Destination: &searchExport, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "with --export, write to file", Destination: &searchExportOut, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a query")
			}
			// Join all positional args so unquoted multi-clause queries
			// like `zot search @author: jolly @title: gossip` work without
			// requiring the user to wrap the whole thing in shell quotes.
			query := strings.Join(cmd.Args().Slice(), " ")
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.Search(query, searchLimit)
			if err != nil {
				return err
			}
			// Need full Fields + Creators hydration before export —
			// Search() only returns list-view metadata.
			if searchExport {
				hydrated, err := hydrateSearchHits(db, items)
				if err != nil {
					return err
				}
				res, err := runLibraryExport(hydrated, "bibtex", searchExportOut)
				if err != nil {
					return err
				}
				cmdutil.Output(cmd, res)
				return nil
			}
			cmdutil.Output(cmd, zot.ListResult{
				Query:   query,
				Count:   len(items),
				Items:   items,
				Library: db.LibraryID(),
			})
			return nil
		},
	}
}

// hydrateSearchHits re-reads each search hit through db.Read to pull the
// full Fields map and Creator list. Search() intentionally returns a
// lightweight list-view row — exporting requires the full item. For a
// typical search (≤50 hits) this is ~50 round-trips, cheap enough not to
// warrant a dedicated bulk ListAll-by-id path.
func hydrateSearchHits(db *local.DB, hits []local.Item) ([]local.Item, error) {
	out := make([]local.Item, 0, len(hits))
	for _, h := range hits {
		full, err := db.Read(h.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, *full)
	}
	return out, nil
}

func readCommand() *cli.Command {
	return &cli.Command{
		Name:        "read",
		Usage:       "Show full details of a single item",
		Description: "$ zot item read ABC12345",
		ArgsUsage:   "<key>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			it, err := db.Read(key)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.ItemResult{Item: *it})
			return nil
		},
	}
}

func listCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Usage:       "List items in your library with optional filters",
		Description: "$ zot item list\n$ zot item list --type journalArticle --limit 25\n$ zot item list --collection ABC12345\n$ zot item list --tag neuroimaging --order title",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by item type (e.g. journalArticle, book)", Destination: &listType, Local: true},
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "filter by collection key", Destination: &listCollection, Local: true},
			&cli.StringFlag{Name: "tag", Usage: "filter by tag name", Destination: &listTag, Local: true},
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max results", Destination: &listLimit, Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Destination: &listOffset, Local: true},
			&cli.StringFlag{Name: "order", Value: "added", Usage: "sort order: added, modified, title", Destination: &listOrder, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			filter := local.ListFilter{
				ItemType:      listType,
				CollectionKey: listCollection,
				Tag:           listTag,
				Limit:         listLimit,
				Offset:        listOffset,
			}
			switch listOrder {
			case "modified":
				filter.OrderBy = local.OrderDateModifiedDesc
			case "title":
				filter.OrderBy = local.OrderTitleAsc
			case "added", "":
				filter.OrderBy = local.OrderDateAddedDesc
			default:
				return cmdutil.UsageErrorf(cmd, "unknown --order %q (want added, modified, or title)", listOrder)
			}

			items, err := db.List(filter)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.ListResult{
				Count:   len(items),
				Items:   items,
				Library: db.LibraryID(),
			})
			return nil
		},
	}
}

func infoCommand() *cli.Command {
	return &cli.Command{
		Name:        "info",
		Usage:       "Show library summary statistics",
		Description: "$ zot info",
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			s, err := db.Stats()
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.StatsResult{
				Stats:   *s,
				DataDir: cfg.DataDir,
				Schema:  db.SchemaVersion(),
			})
			return nil
		},
	}
}

func exportCommand() *cli.Command {
	return &cli.Command{
		Name:        "export",
		Usage:       "Export a citation for an item (csl-json or bibtex)",
		Description: "$ zot item export ABC12345\n$ zot item export ABC12345 --format bibtex\n$ zot item export ABC12345 --format bibtex --out ref.bib",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "csl-json", Usage: "output format: csl-json, bibtex", Destination: &exportFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file instead of stdout", Destination: &exportOut, Local: true},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			it, err := db.Read(key)
			if err != nil {
				return err
			}
			body, err := zot.ExportItem(it, zot.ExportFormat(exportFormat))
			if err != nil {
				return err
			}
			if exportOut != "" {
				if err := os.WriteFile(exportOut, []byte(body+"\n"), 0o644); err != nil {
					return err
				}
				body = fmt.Sprintf("wrote %s to %s", exportFormat, exportOut)
			}
			cmdutil.Output(cmd, zot.ExportResult{Key: key, Format: exportFormat, Body: body})
			return nil
		},
	}
}

func openCommand() *cli.Command {
	return &cli.Command{
		Name:        "open",
		Usage:       "Open an item's attachment in the default viewer",
		Description: "$ zot item open ABC12345",
		ArgsUsage:   "<key>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			cfg, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			it, err := db.Read(key)
			if err != nil {
				return err
			}
			att := zot.PickAttachment(it)
			if att == nil {
				return fmt.Errorf("item %s has no attachments", key)
			}
			path := zot.AttachmentPath(cfg.DataDir, att)
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("attachment file missing: %s", path)
			}
			if err := zot.LaunchFile(path); err != nil {
				cmdutil.Output(cmd, zot.OpenResult{Key: key, Path: path, Launched: false, Message: err.Error()})
				return err
			}
			cmdutil.Output(cmd, zot.OpenResult{Key: key, Path: path, Launched: true, Message: "opened " + att.Filename})
			return nil
		},
	}
}
