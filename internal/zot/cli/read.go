package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/citekey"
	"github.com/sciminds/cli/internal/zot/client"
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
	listRemote     bool

	readRemote bool
	readDOI    string

	searchLimit  int
	searchRemote bool
	searchFull   bool

	exportFormat string
	exportOut    string
)

// openLocalDB loads config, ensures the library scope is resolved (auto-
// selecting / prompting / erroring per ensureLibraryScope), opens the local
// zotero.sqlite scoped accordingly, and warns if the schema version is
// outside the tested range.
func openLocalDB(ctx context.Context) (*zot.Config, local.Reader, error) {
	cfg, err := zot.RequireConfig()
	if err != nil {
		return nil, nil, err
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	sel, err := localSelectorFor(cfg, ref)
	if err != nil {
		return nil, nil, err
	}
	db, err := local.Open(cfg.DataDir, sel)
	if err != nil {
		return nil, nil, err
	}
	if db.SchemaOutOfRange() {
		fmt.Fprintf(os.Stderr, "  %s Zotero schema version %d is outside the tested range [%d, %d] — proceeding anyway\n",
			uikit.SymArrow, db.SchemaVersion(), local.MinTestedSchemaVersion, local.MaxTestedSchemaVersion)
	}
	return cfg, db, nil
}

// localSelectorFor picks a local.LibrarySelector for the resolved ref.
// Shared scope resolves the group's SQLite libraryID via the groups table
// (see local.ForGroupByAPIID).
func localSelectorFor(cfg *zot.Config, ref zot.LibraryRef) (local.LibrarySelector, error) {
	switch ref.Scope {
	case zot.LibPersonal:
		return local.ForPersonal(), nil
	case zot.LibShared:
		if cfg.SharedGroupID == "" {
			return local.LibrarySelector{}, fmt.Errorf("--library shared: SharedGroupID is empty (run 'sci zot setup' to auto-detect)")
		}
		apiID, err := strconv.ParseInt(cfg.SharedGroupID, 10, 64)
		if err != nil {
			return local.LibrarySelector{}, fmt.Errorf("parse SharedGroupID %q: %w", cfg.SharedGroupID, err)
		}
		return local.ForGroupByAPIID(apiID), nil
	default:
		return local.LibrarySelector{}, fmt.Errorf("unknown library scope %q", ref.Scope)
	}
}

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:  "search",
		Usage: "Search your library by title, DOI, publication, or @field: clauses",
		Description: "Free text searches title/DOI/publication/creators. Prefix a\n" +
			"clause with @field: to scope it — fields: author, title, doi,\n" +
			"pub, tag, type, year. Clauses AND by default; `|` separates OR\n" +
			"groups; a leading `-` in the value negates the clause.\n\n" +
			"$ sci zot search \"large language models\"\n" +
			"$ sci zot search --limit 100 neuroimaging\n" +
			"$ sci zot search '@tag: Generative_Agents'      # items carrying this tag\n" +
			"$ sci zot search '@author: saxe @year: 2022'    # ANDed clauses\n" +
			"$ sci zot search '@type: book | @type: thesis'  # OR across clauses\n" +
			"$ sci zot search attention --export --out hits.bib\n" +
			"$ sci zot search llm --remote   # Zotero Web API fulltext search (title + creators + year + abstract + notes + PDFs)",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max results", Destination: &searchLimit, Local: true},
			// --export routes the hit list through the same pipeline as
			// `zot export`. Bool flag — always emits bibtex, which is
			// the format this feature exists to serve. Users who want
			// CSL-JSON should use the top-level `zot export` command.
			&cli.BoolFlag{Name: "export", Usage: "emit results as bibtex instead of the normal hit list", Destination: &searchExport, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "with --export, write to file", Destination: &searchExportOut, Local: true},
			&cli.BoolFlag{Name: "notes", Usage: "only show items that have docling extraction notes (local only)", Destination: &searchNotes, Local: true},
			&cli.BoolFlag{Name: "remote", Usage: "hit the Zotero Web API with qmode=everything (matches abstract + fulltext + notes)", Destination: &searchRemote, Local: true},
			&cli.BoolFlag{Name: "full", Aliases: []string{"f"}, Usage: "hydrate each hit with abstract + citekey + authors (one extra local read per hit)", Destination: &searchFull, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a query")
			}
			if searchExportOut != "" && !searchExport {
				return cmdutil.UsageErrorf(cmd, "--out requires --export")
			}
			if searchRemote && searchExport {
				return cmdutil.UsageErrorf(cmd, "--remote and --export are mutually exclusive (export needs full local hydration)")
			}
			if searchRemote && searchNotes {
				return cmdutil.UsageErrorf(cmd, "--notes is local-only; drop it or drop --remote")
			}
			if searchFull && searchExport {
				return cmdutil.UsageErrorf(cmd, "--full and --export are mutually exclusive (use one or the other)")
			}
			// Join all positional args so unquoted multi-clause queries
			// like `zot search @author: jolly @title: gossip` work without
			// requiring the user to wrap the whole thing in shell quotes.
			query := strings.Join(cmd.Args().Slice(), " ")

			if searchRemote {
				c, err := requireAPIClient(ctx)
				if err != nil {
					return err
				}
				raw, err := c.ListItems(ctx, api.ListItemsOptions{
					Query: query,
					QMode: "everything",
					Limit: searchLimit,
				})
				if err != nil {
					return err
				}
				items := lo.Map(raw, func(it client.Item, _ int) local.Item {
					return api.ItemFromClient(&it)
				})
				// Remote items already carry abstract + citekey fields
				// from the API, so --full just reshapes — no extra fetch.
				if searchFull {
					briefs := lo.Map(items, func(it local.Item, _ int) zot.ItemBrief {
						return zot.ToBrief(&it)
					})
					outputScoped(ctx, cmd, zot.ListBriefResult{
						Query: query,
						Count: len(briefs),
						Items: briefs,
						Scope: "title, creators, year, abstract, fulltext, notes (remote)",
					})
					return nil
				}
				outputScoped(ctx, cmd, zot.ListResult{
					Query: query,
					Count: len(items),
					Items: items,
					Scope: "title, creators, year, abstract, fulltext, notes (remote)",
				})
				return nil
			}

			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			items, err := db.Search(query, searchLimit)
			if err != nil {
				return err
			}
			if searchNotes {
				hasNotes, err := db.ParentsWithDoclingNotes()
				if err != nil {
					return err
				}
				items = lo.Filter(items, func(it local.Item, _ int) bool {
					return hasNotes[it.Key]
				})
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
				outputScoped(ctx, cmd, res)
				return nil
			}
			if searchFull {
				hydrated, err := hydrateSearchHits(db, items)
				if err != nil {
					return err
				}
				briefs := lo.Map(hydrated, func(it local.Item, _ int) zot.ItemBrief {
					return zot.ToBrief(&it)
				})
				bres := zot.ListBriefResult{
					Query:   query,
					Count:   len(briefs),
					Items:   briefs,
					Library: db.LibraryID(),
				}
				if len(briefs) == 0 {
					bres.Scope = "title, DOI, publication, creators (local)"
					bres.Hint = "try --remote to also match abstract, fulltext, and notes"
				}
				outputScoped(ctx, cmd, bres)
				return nil
			}
			res := zot.ListResult{
				Query:   query,
				Count:   len(items),
				Items:   items,
				Library: db.LibraryID(),
			}
			if len(items) == 0 {
				res.Scope = "title, DOI, publication, creators (local)"
				res.Hint = "try --remote to also match abstract, fulltext, and notes"
			}
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}

// hydrateSearchHits re-reads each search hit through db.Read to pull the
// full Fields map and Creator list. Search() intentionally returns a
// lightweight list-view row — exporting requires the full item. For a
// typical search (≤50 hits) this is ~50 round-trips, cheap enough not to
// warrant a dedicated bulk ListAll-by-id path.
func hydrateSearchHits(db local.Reader, hits []local.Item) ([]local.Item, error) {
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
		Name:  "read",
		Usage: "Show full details of a single item by key or DOI",
		Description: "$ sci zot item read ABC12345\n" +
			"$ sci zot item read --doi 10.1038/nature12373\n" +
			"$ sci zot item read ABC12345 --remote   # bypass local SQLite, hit the Zotero Web API",
		ArgsUsage: "<key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API instead of the local SQLite (for items not yet synced)", Destination: &readRemote, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "look up the item by DOI instead of key (case-insensitive; local-only — try `find works <doi>` for OpenAlex)", Destination: &readDOI, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			argKey := ""
			if cmd.Args().Len() > 0 {
				argKey = cmd.Args().First()
			}
			switch {
			case readDOI != "" && argKey != "":
				return cmdutil.UsageErrorf(cmd, "pass either a key positional or --doi, not both")
			case readDOI == "" && argKey == "":
				return cmdutil.UsageErrorf(cmd, "expected an item key or --doi <doi>")
			}

			// DOI lookup is always local-first: ItemKeysByDOI hits SQLite,
			// then we either render the local item or, if --remote was
			// passed, re-fetch the resolved key over the Web API for fresh
			// data. Resolving DOI → key remotely (search the API by DOI)
			// would be a different feature; the agent UX win is "I have
			// the DOI, give me the key + body" without a manual search step.
			key := argKey
			if readDOI != "" {
				_, db, err := openLocalDB(ctx)
				if err != nil {
					return err
				}
				hits, derr := db.ItemKeysByDOI([]string{readDOI})
				_ = db.Close()
				if derr != nil {
					return derr
				}
				resolved, ok := hits[strings.ToLower(readDOI)]
				if !ok {
					return fmt.Errorf("no item with DOI %q in library — use `sci zot find works %q` to look it up on OpenAlex", readDOI, readDOI)
				}
				key = resolved
			}

			if readRemote {
				c, err := requireAPIClient(ctx)
				if err != nil {
					return err
				}
				raw, err := c.GetItem(ctx, key)
				if err != nil {
					return err
				}
				it := api.ItemFromClient(raw)
				citekey.Enrich(&it)
				outputScoped(ctx, cmd, zot.ItemResult{Item: it})
				return nil
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			it, err := db.Read(key)
			if err != nil {
				return fmt.Errorf("%w (pass --remote to bypass local sqlite if the item was just created)", err)
			}
			citekey.Enrich(it)
			outputScoped(ctx, cmd, zot.ItemResult{Item: *it})
			return nil
		},
	}
}

func listCommand() *cli.Command {
	return &cli.Command{
		Name:        "list",
		Usage:       "List items in your library with optional filters",
		Description: "$ sci zot item list\n$ sci zot item list --type journalArticle --limit 25\n$ sci zot item list --collection ABC12345\n$ sci zot item list --tag neuroimaging --order title\n$ sci zot item list --collection ABC12345 --remote   # bypass local SQLite (for items not yet synced)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "filter by item type (e.g. journalArticle, book)", Destination: &listType, Local: true},
			&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "filter by collection key", Destination: &listCollection, Local: true},
			&cli.StringFlag{Name: "tag", Usage: "filter by tag name (local only)", Destination: &listTag, Local: true},
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 25, Usage: "max results", Destination: &listLimit, Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Destination: &listOffset, Local: true},
			&cli.StringFlag{Name: "order", Value: "added", Usage: "sort order: added, modified, title (local only)", Destination: &listOrder, Local: true},
			&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API (shows items not yet synced locally)", Destination: &listRemote, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if listRemote {
				if listTag != "" {
					return cmdutil.UsageErrorf(cmd, "--tag is local-only; drop it or drop --remote")
				}
				c, err := requireAPIClient(ctx)
				if err != nil {
					return err
				}
				raw, err := c.ListItems(ctx, api.ListItemsOptions{
					CollectionKey: listCollection,
					ItemType:      listType,
					Start:         listOffset,
					Limit:         listLimit,
				})
				if err != nil {
					return err
				}
				items := lo.Map(raw, func(it client.Item, _ int) local.Item {
					out := api.ItemFromClient(&it)
					citekey.Enrich(&out)
					return out
				})
				outputScoped(ctx, cmd, zot.ListResult{
					Count: len(items),
					Items: items,
				})
				return nil
			}

			_, db, err := openLocalDB(ctx)
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
			result := zot.ListResult{
				Count:   len(items),
				Items:   items,
				Library: db.LibraryID(),
			}
			// Empty-result heuristic: if the user asked for a specific
			// collection that the local DB doesn't know about, the most
			// likely cause is "Zotero desktop hasn't synced yet" — surface
			// the --remote escape hatch so agents don't conclude "no items"
			// silently. A known-but-empty collection stays quiet (legit).
			if len(items) == 0 && listCollection != "" {
				if c, lerr := db.CollectionByKey(listCollection); lerr == nil && c == nil {
					result.Hint = "collection " + listCollection + " not found locally; pass --remote to fetch from the Zotero Web API (items just created may not be synced yet)"
				}
			}
			outputScoped(ctx, cmd, result)
			return nil
		},
	}
}

var infoOrient bool

func infoCommand() *cli.Command {
	return &cli.Command{
		Name:  "info",
		Usage: "Show library summary statistics",
		Description: "$ sci zot info                       # summarize both libraries\n" +
			"$ sci zot info --library personal    # narrow to personal\n" +
			"$ sci zot info --library shared      # narrow to shared\n" +
			"$ sci zot info --orient              # add agent-bootstrap signals (top tags/collections, recent items, has-markdown coverage)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "orient", Usage: "include top tags + top collections + recent items + has-markdown extraction coverage", Destination: &infoOrient, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := zot.RequireConfig()
			if err != nil {
				return err
			}
			// Flag supplied → narrow to one library.
			if _, ok := LibraryFromContext(ctx); ok {
				entry, err := statsForScope(ctx, cfg)
				if err != nil {
					return err
				}
				outputScoped(ctx, cmd, entry)
				return nil
			}
			// No flag → summarize every library the account has access to.
			return runInfoAllLibraries(ctx, cmd, cfg)
		},
	}
}

// statsForScope opens the local DB scoped to ctx's library ref and returns
// a single StatsResult labeled with the scope. Used by `info --library …`
// (single scope) and runInfoAllLibraries (loops once per library).
func statsForScope(ctx context.Context, cfg *zot.Config) (zot.StatsResult, error) {
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return zot.StatsResult{}, err
	}
	return statsForRef(cfg, ref)
}

// statsForRef opens the local DB for an explicit ref. Lets the multi-library
// path (runInfoAllLibraries) iterate without re-priming a holder per call.
// Reads the package-level infoOrient flag to decide whether to populate
// the agent-bootstrap signals — keeps the call sites uniform across the
// flag-set / multi-library / orient combinations.
func statsForRef(cfg *zot.Config, ref zot.LibraryRef) (zot.StatsResult, error) {
	sel, err := localSelectorFor(cfg, ref)
	if err != nil {
		return zot.StatsResult{}, err
	}
	db, err := local.Open(cfg.DataDir, sel)
	if err != nil {
		return zot.StatsResult{}, err
	}
	defer func() { _ = db.Close() }()
	s, err := db.Stats()
	if err != nil {
		return zot.StatsResult{}, err
	}
	label := "personal"
	scope := "personal"
	apiID := cfg.UserID
	if ref.Scope == zot.LibShared {
		label = "shared"
		scope = "shared"
		apiID = cfg.SharedGroupID
		if cfg.SharedGroupName != "" {
			label = "shared (" + cfg.SharedGroupName + ")"
		}
	}
	out := zot.StatsResult{
		Library:      label,
		Scope:        scope,
		LibraryAPIID: apiID,
		Stats:        *s,
		DataDir:      cfg.DataDir,
		Schema:       db.SchemaVersion(),
	}
	if infoOrient {
		if err := populateOrient(db, &out); err != nil {
			return zot.StatsResult{}, err
		}
	}
	return out, nil
}

// populateOrient fills the agent-bootstrap fields. Defaults: top 10 tags,
// top 10 collections, last 5 items added. Counts large enough to be
// useful as a snapshot; small enough to stay in a few hundred tokens.
func populateOrient(db local.Reader, out *zot.StatsResult) error {
	cov, err := db.ExtractionCoverage()
	if err != nil {
		return err
	}
	out.ExtractionCoverage = cov

	tags, err := db.TopTags(10)
	if err != nil {
		return err
	}
	out.TopTags = tags

	colls, err := db.TopCollections(10)
	if err != nil {
		return err
	}
	out.TopCollections = colls

	recent, err := db.RecentlyAdded(5)
	if err != nil {
		return err
	}
	out.RecentAdded = recent
	return nil
}

// runInfoAllLibraries gathers stats for every library the account has
// access to. Shared-library failures are collected as non-fatal errors so
// personal still renders when the group isn't synced yet.
func runInfoAllLibraries(ctx context.Context, cmd *cli.Command, cfg *zot.Config) error {
	out := zot.MultiStatsResult{}

	if ref, err := cfg.Resolve(zot.LibPersonal); err != nil {
		out.Errors = append(out.Errors, "personal: "+err.Error())
	} else if entry, err := statsForRef(cfg, ref); err != nil {
		out.Errors = append(out.Errors, "personal: "+err.Error())
	} else {
		out.PerLibrary = append(out.PerLibrary, entry)
	}

	if cfg.SharedGroupID != "" {
		if ref, err := cfg.Resolve(zot.LibShared); err != nil {
			out.Errors = append(out.Errors, "shared: "+err.Error())
		} else if entry, err := statsForRef(cfg, ref); err != nil {
			out.Errors = append(out.Errors, "shared: "+err.Error())
		} else {
			out.PerLibrary = append(out.PerLibrary, entry)
		}
	}

	outputScoped(ctx, cmd, out)
	return nil
}

func exportCommand() *cli.Command {
	return &cli.Command{
		Name:        "export",
		Usage:       "Export a citation for an item (csl-json or bibtex)",
		Description: "$ sci zot item export ABC12345\n$ sci zot item export ABC12345 --format bibtex\n$ sci zot item export ABC12345 --format bibtex --out ref.bib",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "csl-json", Usage: "output format: csl-json, bibtex", Destination: &exportFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "write to file instead of stdout", Destination: &exportOut, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			_, db, err := openLocalDB(ctx)
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
			outputScoped(ctx, cmd, zot.ExportResult{Key: key, Format: exportFormat, Body: body})
			return nil
		},
	}
}

func childrenCommand() *cli.Command {
	return &cli.Command{
		Name:  "children",
		Usage: "List the child items (attachments + notes) of a parent item",
		Description: "$ sci zot item children 6R45EVSB\n" +
			"$ sci zot --json item children 6R45EVSB | jq '.children[] | select(.item_type==\"note\") | .key'\n" +
			"\n" +
			"Lists every child from the local Zotero database. Use together with\n" +
			"`zot item delete` to prune specific notes or attachments.",
		ArgsUsage: "<parent-item-key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one item key")
			}
			parentKey := cmd.Args().First()
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			children, err := db.ListChildren(parentKey)
			if err != nil {
				return err
			}
			views := lo.Map(children, func(ch local.ChildItem, _ int) zot.ChildItemView {
				return toChildItemView(ch)
			})
			outputScoped(ctx, cmd, zot.ChildrenListResult{
				ParentKey: parentKey,
				Count:     len(views),
				Children:  views,
			})
			return nil
		},
	}
}

// toChildItemView projects a local.ChildItem into the zot-package
// mirror type used by ChildrenListResult. The duplication exists to
// break the local → zot import cycle; see zot.ChildItemView's doc.
func toChildItemView(ch local.ChildItem) zot.ChildItemView {
	return zot.ChildItemView{
		Key:         ch.Key,
		ItemType:    ch.ItemType,
		Title:       ch.Title,
		Note:        ch.Note,
		ContentType: ch.ContentType,
		Filename:    ch.Filename,
		Tags:        ch.Tags,
	}
}

func openCommand() *cli.Command {
	return &cli.Command{
		Name:        "open",
		Usage:       "Open an item's attachment in the default viewer",
		Description: "$ sci zot item open ABC12345",
		ArgsUsage:   "<key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			cfg, db, err := openLocalDB(ctx)
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
				outputScoped(ctx, cmd, zot.OpenResult{Key: key, Path: path, Launched: false, Message: err.Error()})
				return err
			}
			outputScoped(ctx, cmd, zot.OpenResult{Key: key, Path: path, Launched: true, Message: "opened " + att.Filename})
			return nil
		},
	}
}
