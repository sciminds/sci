package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
	"github.com/sciminds/sci/internal/zot/content"
	"github.com/sciminds/sci/pkg/citekey"
	"github.com/sciminds/sci/pkg/local"
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

	readRemote    bool
	readDOI       string
	readMissingOK bool

	childrenRemote bool

	searchLimit    int
	searchRemote   bool
	searchFull     bool
	searchContent  bool
	searchFulltext bool // retired — see retiredSearchFlagError
	searchNoteText bool // retired — see retiredSearchFlagError

	exportFormat string
	exportOut    string
)

// openLocalDB loads config, ensures the library scope is resolved (auto-
// selecting / prompting / erroring per ensureLibraryScope), opens the local
// zotero.sqlite scoped accordingly, and warns if the schema version is
// outside the tested range.
func openLocalDB(ctx context.Context) (*zot.Config, local.Reader, error) {
	return openLocalDBScoped(ctx, false)
}

// openLocalDBAllowAll is openLocalDB for the commands that opted into
// the merged --library all pool (search, bib). Everything else goes
// through openLocalDB, which rejects `all` with the rewrite hint —
// their query paths still bind a single libraryID and would silently
// answer personal-only.
func openLocalDBAllowAll(ctx context.Context) (*zot.Config, local.Reader, error) {
	return openLocalDBScoped(ctx, true)
}

func openLocalDBScoped(ctx context.Context, allowAll bool) (*zot.Config, local.Reader, error) {
	cfg, err := requireConfigCoded()
	if err != nil {
		return nil, nil, err
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	if ref.Scope == zot.LibAll && !allowAll {
		return nil, nil, cmdutil.Coded(cmdutil.CodeUsage,
			"--library all is supported by search and bib only (so far) — this command reads a single library").
			WithTry("re-run with --library personal or --library shared")
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
	// Connection-level freshness, reported here rather than per-command: the
	// caveat applies to every read on this handle, and the commands that
	// most needed it (doctor, hygiene) are exactly the ones that never wired
	// up a per-result warning. Reuses walStaleWarning so the prose can't
	// drift from the enveloped copy. Diagnostics go to stderr, so this is
	// safe under --json.
	for _, w := range walStaleWarning(db) {
		fmt.Fprintf(os.Stderr, "  %s %s — %s\n", uikit.SymArrow, w.Message, w.Fix)
	}
	return cfg, db, nil
}

// tryOpenLocalDB is the non-prompting flavor of openLocalDB. It opens
// the local store only when the library scope is resolvable without
// asking the user (i.e. --library was passed, or only one library is
// configured). Returns ok=false when any prerequisite is missing — the
// caller is expected to fall back gracefully (e.g. skip an
// opportunistic enrichment) rather than error.
//
// Used by `find works` to mark in-library hits without forcing every
// invocation through library scope resolution.
func tryOpenLocalDB(ctx context.Context) (local.Reader, bool) {
	holder := libraryHolderFromCtx(ctx)
	if holder == nil {
		return nil, false
	}
	cfg, err := zot.LoadConfig()
	if err != nil || cfg == nil {
		return nil, false
	}
	// If --library wasn't set and both libraries are configured, resolving
	// would prompt (interactive) or error (--json / non-TTY). Bail.
	if !holder.HasFlag && cfg.SharedGroupID != "" {
		return nil, false
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, false
	}
	// The opportunistic callers behind this helper still bind a single
	// libraryID — under a merged scope, skipping the enrichment honestly
	// beats silently enriching against the personal library only.
	if ref.Scope == zot.LibAll {
		return nil, false
	}
	sel, err := localSelectorFor(cfg, ref)
	if err != nil {
		return nil, false
	}
	db, err := local.Open(cfg.DataDir, sel)
	if err != nil {
		return nil, false
	}
	return db, true
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
	case zot.LibAll:
		if cfg.SharedGroupID == "" {
			return local.LibrarySelector{}, fmt.Errorf("--library all: SharedGroupID is empty (run 'sci zot setup' to auto-detect)")
		}
		apiID, err := strconv.ParseInt(cfg.SharedGroupID, 10, 64)
		if err != nil {
			return local.LibrarySelector{}, fmt.Errorf("parse SharedGroupID %q: %w", cfg.SharedGroupID, err)
		}
		return local.ForAll(apiID), nil
	default:
		return local.LibrarySelector{}, fmt.Errorf("unknown library scope %q", ref.Scope)
	}
}

func searchCommand() *cli.Command {
	return &cli.Command{
		Name:  "search",
		Usage: "Search your library by title, DOI, publication, cite-key, or @field: clauses",
		Description: "Free text searches title/DOI/publication/creators/cite-keys,\n" +
			"ranked by title relevance, then (with --content) how well the\n" +
			"paper's text matches, then year. Prefix a clause with\n" +
			"@field: to scope it — fields: author, title, doi, pub, tag,\n" +
			"type, year, citekey. Bare prefixes work too: tag:read means\n" +
			"@tag: read, and -tag:read negates it. Clauses AND by default;\n" +
			"`|` separates OR groups; a leading `-` in the value negates\n" +
			"the clause.\n\n" +
			"$ sci zot search 'cortex -tag:to-read'           # free text minus a tag\n" +
			"$ sci zot search \"large language models\"\n" +
			"$ sci zot search --limit 100 neuroimaging\n" +
			"$ sci zot search '@tag: Generative_Agents'      # items carrying this tag\n" +
			"$ sci zot search '@author: saxe @year: 2022'    # ANDed clauses\n" +
			"$ sci zot search '@type: book | @type: thesis'  # OR across clauses\n" +
			"$ sci zot search '@citekey: saxe2022-ment'      # which paper is this key?\n" +
			"$ sci zot search cortical --content             # also match the text of your papers\n" +
			"$ sci zot search '\"prediction error\"' --content  # quoted = phrase, not two words\n" +
			"$ sci zot search attention --export --out hits.bib\n" +
			"$ sci zot search llm --remote   # Zotero Web API fulltext search (title + creators + year + abstract + notes + PDFs)",
		ArgsUsage: "<query>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max results", Destination: &searchLimit, Local: true},
			// --export routes the hit list through the same pipeline as
			// `zot export`.
			&cli.BoolFlag{Name: "export", Usage: "emit results as a bibliography instead of the normal hit list", Destination: &searchExport, Local: true},
			&cli.StringFlag{Name: "format", Usage: "with --export, output format: biblatex (alias: bibtex), csl-json", Value: "biblatex", Destination: &searchExportFormat, Local: true},
			&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "with --export, write to file", Destination: &searchExportOut, Local: true},
			&cli.BoolFlag{Name: "notes", Usage: "filter to items that HAVE a docling extraction", Destination: &searchNotes, Local: true},
			&cli.BoolFlag{Name: "content", Usage: "also match free-text terms against the full text of your papers (needs `sci zot content build`) — local only", Destination: &searchContent, Local: true},
			// Retired in favor of --content, which subsumes both. Kept as
			// hidden flags purely so the error can name the replacement
			// instead of urfave's bare "flag provided but not defined".
			&cli.BoolFlag{Name: "fulltext", Hidden: true, Destination: &searchFulltext, Local: true},
			&cli.BoolFlag{Name: "note-text", Hidden: true, Destination: &searchNoteText, Local: true},
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
				return cmdutil.Coded(cmdutil.CodeConflict, "--remote and --export are mutually exclusive (export needs full local hydration)").
					WithTry("drop --remote; export always hydrates from the local library")
			}
			if searchRemote && searchNotes {
				return cmdutil.Coded(cmdutil.CodeConflict, "--notes is local-only").
					WithTry("drop --notes or drop --remote (--remote already matches note text server-side)")
			}
			if err := retiredSearchFlagError(); err != nil {
				return err
			}
			if searchRemote && searchContent {
				return cmdutil.Coded(cmdutil.CodeConflict, "--content is local-only").
					WithTry("drop --content; --remote already matches PDF text and notes server-side")
			}
			if err := searchAllConflicts(ctx); err != nil {
				return err
			}
			if searchFull && searchExport {
				return cmdutil.Coded(cmdutil.CodeConflict, "--full and --export are mutually exclusive").
					WithTry("use --full for reading hits inline, --export for generating a bibliography")
			}
			// Join all positional args so unquoted multi-clause queries
			// like `zot search @author: jolly @title: gossip` work without
			// requiring the user to wrap the whole thing in shell quotes.
			query := strings.Join(cmd.Args().Slice(), " ")
			mergedScope := requestedScopeIsAll(ctx)

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

			_, db, err := openLocalDBAllowAll(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			var opts local.SearchOptions
			var contentWarns []cmdutil.Warning
			var csearch *contentSearch
			if searchContent {
				csearch, err = contentWidener(ctx, db)
				if err != nil {
					return err
				}
				defer csearch.close()
				opts.Content, contentWarns = csearch.widen, csearch.warns
			}

			items, total, err := db.SearchWithTotal(query, searchLimit, opts)
			if err != nil {
				return err
			}
			// Freshness caveat rides every local hit list; the fix is the
			// same command against API ground truth (except --export, whose
			// pipeline is local-only, and --library all, where --remote
			// would conflict — a Fix must be resubmittable verbatim).
			rerunFix := remoteRerunFix(os.Args)
			if mergedScope {
				rerunFix = ""
			}
			staleWarns := append(localReadWarnings(db, rerunFix), contentWarns...)
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
				res, err := runLibraryExport(hydrated, searchExportFormat, searchExportOut)
				if err != nil {
					return err
				}
				outputScoped(ctx, cmd, cmdutil.WithWarnings(res, localReadWarnings(db, "")...))
				return nil
			}
			// Excerpts are fetched only for the hits that survived ranking
			// and filtering — building one reads the paper's whole body.
			var snippets map[string]string
			if csearch != nil {
				snippets = csearch.snippets(query, lo.Map(items, func(it local.Item, _ int) string {
					return it.Key
				}))
				snippets = dropTitleEchoes(snippets, items)
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
					Query:     query,
					Count:     len(briefs),
					Total:     total,
					Truncated: len(briefs) < total,
					Items:     briefs,
					Library:   db.LibraryID(),
					Snippets:  snippets,
				}
				if len(briefs) == 0 {
					bres.Scope = localSearchScope(searchContent)
					bres.Hint = localSearchHint(searchContent)
				}
				outputScoped(ctx, cmd, cmdutil.WithWarnings(bres, staleWarns...))
				return nil
			}
			res := zot.ListResult{
				Query:     query,
				Count:     len(items),
				Total:     total,
				Truncated: len(items) < total,
				Items:     items,
				Library:   db.LibraryID(),
				Snippets:  snippets,
			}
			if len(items) == 0 {
				res.Scope = localSearchScope(searchContent)
				res.Hint = localSearchHint(searchContent)
			}
			outputScoped(ctx, cmd, cmdutil.WithWarnings(res, staleWarns...))
			return nil
		},
	}
}

// requestedScopeIsAll reports whether this invocation asked for the
// merged --library all pool. Read off the pre-resolution holder so
// conflict checks can fire before any config or DB work.
func requestedScopeIsAll(ctx context.Context) bool {
	ref, ok := LibraryFromContext(ctx)
	return ok && ref.Scope == zot.LibAll
}

// searchAllConflicts rejects the search flags that cannot honestly serve
// a merged pool — each would silently answer against a single library
// (or none), which is worse than an error that names the limitation.
func searchAllConflicts(ctx context.Context) error {
	if !requestedScopeIsAll(ctx) {
		return nil
	}
	switch {
	case searchContent:
		return cmdutil.Coded(cmdutil.CodeConflict, "--content is per-library (each library has its own text index) and cannot serve --library all").
			WithTry("run --content against one library at a time, or drop --content")
	case searchRemote:
		return cmdutil.Coded(cmdutil.CodeConflict, "--remote has no merged endpoint — the Zotero Web API serves one library per call").
			WithTry("fan out --remote per library, or drop --remote")
	case searchNotes:
		return cmdutil.Coded(cmdutil.CodeConflict, "--notes filters per-library and cannot serve --library all yet").
			WithTry("run --notes against one library at a time, or drop --notes")
	}
	return nil
}

// hydrateSearchHits re-reads the search hits through one batched ListAll
// call to pull the full Fields map and Creator list. Search() intentionally
// returns a lightweight list-view row — exporting requires the full item.
// The batch keeps hit (relevance) order; ListAll's own ordering is
// discarded by reindexing on key.
func hydrateSearchHits(db local.Reader, hits []local.Item) ([]local.Item, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	keys := lo.Map(hits, func(h local.Item, _ int) string { return h.Key })
	full, err := db.ListAll(local.ListFilter{Keys: keys})
	if err != nil {
		return nil, err
	}
	byKey := lo.KeyBy(full, func(it local.Item) string { return it.Key })
	return lo.FilterMap(hits, func(h local.Item, _ int) (local.Item, bool) {
		it, ok := byKey[h.Key]
		return it, ok
	}), nil
}

func readCommand() *cli.Command {
	return &cli.Command{
		Name:  "read",
		Usage: "Show full details of one or more items by key or DOI",
		Description: "One key returns the bare item; several keys return\n" +
			"{count, items} in request order. A missing key fails the whole\n" +
			"read naming it — never a silent partial result.\n\n" +
			"$ sci zot item read ABC12345\n" +
			"$ sci zot item read ABC12345 DEF67890 GHI13579   # batch read\n" +
			"$ sci zot item read --doi 10.1038/nature12373\n" +
			"$ sci zot item read ABC12345 --remote   # bypass local SQLite, hit the Zotero Web API",
		ArgsUsage: "<key> [key...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API instead of the local SQLite (for items not yet synced)", Destination: &readRemote, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "look up the item by DOI instead of key (case-insensitive; accepts bare 10.x/y, https://doi.org/..., or doi:... forms; local-only — try `find works <doi>` for OpenAlex)", Destination: &readDOI, Local: true},
			&cli.BoolFlag{Name: "missing-ok", Usage: "return the items that exist and report the rest in data.missing instead of failing the whole batch; always emits the {count, items} wrapper", Destination: &readMissingOK, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keys := cmd.Args().Slice()
			switch {
			case readDOI != "" && len(keys) > 0:
				return cmdutil.UsageErrorf(cmd, "pass either key positionals or --doi, not both")
			case readDOI == "" && len(keys) == 0:
				return cmdutil.UsageErrorf(cmd, "expected one or more item keys or --doi <doi>")
			}

			// DOI lookup is always local-first: ItemKeysByDOI hits SQLite,
			// then we either render the local item or, if --remote was
			// passed, re-fetch the resolved key over the Web API for fresh
			// data. Resolving DOI → key remotely (search the API by DOI)
			// would be a different feature; the agent UX win is "I have
			// the DOI, give me the key + body" without a manual search step.
			if readDOI != "" {
				normDOI := normalizeDOI(readDOI)
				if normDOI == "" {
					return fmt.Errorf("--doi value %q is empty after stripping URL/`doi:` prefix", readDOI)
				}
				_, db, err := openLocalDB(ctx)
				if err != nil {
					return err
				}
				hits, derr := db.ItemKeysByDOI([]string{normDOI})
				_ = db.Close()
				if derr != nil {
					return derr
				}
				resolved, ok := hits[strings.ToLower(normDOI)]
				if !ok {
					return fmt.Errorf("no item with DOI %q in library — use `sci zot find works %q` to look it up on OpenAlex", normDOI, normDOI)
				}
				keys = []string{resolved}
			}

			// A positional that isn't an 8-char Zotero key is very likely a
			// cite key (agents paste them from bibliographies) — absorb it
			// by resolving against the local library instead of erroring.
			keys = lo.Map(keys, func(key string, _ int) string {
				if !zoteroKeyRE.MatchString(key) {
					if resolved := resolveCiteKeyArg(ctx, key); resolved != "" {
						return resolved
					}
				}
				return key
			})

			if readRemote {
				c, err := requireAPIClient(ctx)
				if err != nil {
					return err
				}
				// Only a genuine 404 is a collectible miss under --missing-ok;
				// a transport failure aborts the batch — reporting it as
				// "missing" would launder an outage into data.
				var missing []string
				var callErr error
				items := lo.FilterMap(keys, func(key string, _ int) (local.Item, bool) {
					if callErr != nil {
						return local.Item{}, false
					}
					raw, err := c.GetItem(ctx, key)
					switch {
					case readMissingOK && errors.Is(err, api.ErrNotFound):
						missing = append(missing, key)
						return local.Item{}, false
					case err != nil:
						callErr = err
						return local.Item{}, false
					}
					it := api.ItemFromClient(raw)
					citekey.Enrich(&it)
					labelRemoteRelations(ctx, &it)
					return it, true
				})
				if callErr != nil {
					return callErr
				}
				if readMissingOK {
					res := zot.ItemsResult{Count: len(items), Items: items, Missing: missing}
					outputScoped(ctx, cmd, cmdutil.WithWarnings(res, missingKeysWarning(missing)...))
					return nil
				}
				outputScoped(ctx, cmd, readResultFor(items))
				return nil
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			// A missing key fails the whole batch, naming every straggler —
			// a partial result would be the same silent drop the multi-key
			// form exists to fix. readErr keeps the single-key error shape
			// (it wraps the underlying DB error) byte-compatible.
			var missing []string
			var readErr error
			items := lo.FilterMap(keys, func(key string, _ int) (local.Item, bool) {
				it, err := db.Read(key)
				if err != nil {
					missing = append(missing, key)
					readErr = err
					return local.Item{}, false
				}
				citekey.Enrich(it)
				return *it, true
			})
			switch {
			case readMissingOK:
				// Partial-with-report: the found items ship, the misses are
				// data (data.missing) AND a warning, and the wrapper shape is
				// unconditional so batch callers never branch on arity.
				res := zot.ItemsResult{Count: len(items), Items: items, Missing: missing}
				warns := append(localReadWarnings(db, remoteRerunFix(os.Args)),
					missingKeysWarning(missing)...)
				outputScoped(ctx, cmd, cmdutil.WithWarnings(res, warns...))
				return nil
			case len(missing) > 0 && len(keys) == 1:
				return itemNotFoundErr(ctx, keys[0], readErr)
			case len(missing) > 0:
				return itemsNotFoundErr(ctx, keys, missing)
			}
			outputScoped(ctx, cmd, cmdutil.WithWarnings(readResultFor(items),
				localReadWarnings(db, remoteRerunFix(os.Args))...))
			return nil
		},
	}
}

// missingKeysWarning surfaces `--missing-ok` misses on the envelope's
// warnings[] as well as data.missing — belt and braces for tooling that
// only inspects one of the two. Empty misses emit nothing.
func missingKeysWarning(missing []string) []cmdutil.Warning {
	if len(missing) == 0 {
		return nil
	}
	return []cmdutil.Warning{{
		Code:    cmdutil.CodeNotFound,
		Message: fmt.Sprintf("%d key(s) not found: %s", len(missing), strings.Join(missing, ", ")),
	}}
}

// readResultFor picks the result shape by request arity: one key keeps the
// bare-item ItemResult (pinned — existing consumers parse it), several get
// the {count, items} wrapper. The shape follows what was ASKED, so an agent
// scripting `item read $KEYS` can branch on its own argument count.
func readResultFor(items []local.Item) cmdutil.Result {
	if len(items) == 1 {
		return zot.ItemResult{Item: items[0]}
	}
	return zot.ItemsResult{Count: len(items), Items: items}
}

// labelRemoteRelations names the far ends of an item fetched over the Web
// API, best-effort, from the local mirror.
//
// The API knows the relation but not what it points at, and `--remote` is
// exactly the flag you reach for right after `link add` — when the RELATION
// is too new for the local DB but the papers on the other end have been in
// it for months. Failures are swallowed: a bare key still identifies the
// item, and a read that succeeded must not fail over decoration.
func labelRemoteRelations(ctx context.Context, it *local.Item) {
	if it.Relations == nil {
		return
	}
	referenced := lo.Uniq(append(
		lo.Flatten(lo.Values(it.Relations.Other)), it.Relations.Related...))
	if labels := linkTitles(ctx, referenced...); len(labels) > 0 {
		it.Relations.Titles = labels
	}
}

// normalizeDOI strips the URL prefix (https://doi.org/...) and the
// `doi:` scheme prefix from a DOI input, then trims whitespace. Agents
// copy DOIs from many sources (browser bars, paper PDFs, citation
// managers) and the URL form is overwhelmingly the most common —
// failing the lookup just because the user pasted a URL instead of a
// bare "10.x/y" is hostile UX.
//
// Case is preserved here; ItemKeysByDOI matches case-insensitively.
func normalizeDOI(in string) string {
	s := strings.TrimSpace(in)
	for _, p := range []string{
		"https://doi.org/",
		"http://doi.org/",
		"https://dx.doi.org/",
		"http://dx.doi.org/",
		"doi:",
	} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
			break
		}
	}
	return strings.TrimSpace(s)
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
			// Honest denominator: never let a LIMITed page read as the whole
			// library. CountList failure degrades to Total=0 (unknown), never
			// an error — the listing itself succeeded.
			total, cerr := db.CountList(filter)
			if cerr != nil {
				total = 0
			}
			result := zot.ListResult{
				Count:     len(items),
				Total:     total,
				Truncated: total > listOffset+len(items),
				Items:     items,
				Library:   db.LibraryID(),
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
			outputScoped(ctx, cmd, cmdutil.WithWarnings(result, localReadWarnings(db, remoteRerunFix(os.Args))...))
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
			cfg, err := requireConfigCoded()
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
		Usage:       "Export a citation for an item (csl-json or biblatex)",
		Description: "$ sci zot item export ABC12345\n$ sci zot item export ABC12345 --format biblatex\n$ sci zot item export ABC12345 --format biblatex --out ref.bib",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "csl-json", Usage: "output format: csl-json, biblatex (alias: bibtex)", Destination: &exportFormat, Local: true},
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
			format := zot.ExportFormat(exportFormat).Canon()
			body, err := zot.ExportItem(it, format)
			if err != nil {
				return err
			}
			if exportOut != "" {
				if err := os.WriteFile(exportOut, []byte(body+"\n"), 0o644); err != nil {
					return err
				}
				body = fmt.Sprintf("wrote %s to %s", format, exportOut)
			}
			outputScoped(ctx, cmd, zot.ExportResult{Key: key, Format: string(format), Body: body})
			return nil
		},
	}
}

func childrenCommand() *cli.Command {
	return &cli.Command{
		Name:  "children",
		Usage: "List the child items (attachments + notes) of a parent item",
		Description: "$ sci zot item children 6R45EVSB\n" +
			"$ sci zot item children 6R45EVSB --remote   # bypass local sqlite; ground truth\n" +
			"$ sci zot --json item children 6R45EVSB | jq '.children[] | select(.item_type==\"note\") | .key'\n" +
			"\n" +
			"Reads the local Zotero database by default. Pass --remote when the\n" +
			"answer must reflect writes this CLI just made — the local mirror does\n" +
			"not see them until Zotero desktop syncs, so a local read of a\n" +
			"freshly-attached parent reports zero children. Use together with\n" +
			"`zot item delete` to prune specific notes or attachments.",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API instead of the local SQLite (sees children written since the last desktop sync)", Destination: &childrenRemote, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one item key")
			}
			parentKey := cmd.Args().First()
			if childrenRemote {
				return runChildrenRemote(ctx, cmd, parentKey)
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			// Validate the parent exists before reporting "no children".
			// Without this, a typo'd key returns a misleading "→ X has no
			// children" response — caller assumes the parent is real and
			// childless when actually it never existed.
			if _, err := db.Read(parentKey); err != nil {
				return fmt.Errorf("%w (pass --remote to bypass local sqlite if the item was just created)", err)
			}

			children, err := db.ListChildren(parentKey)
			if err != nil {
				return err
			}
			views := lo.Map(children, toChildItemView)
			result := zot.ChildrenListResult{
				ParentKey: parentKey,
				Count:     len(views),
				Children:  views,
			}
			warns := append(localReadWarnings(db, remoteRerunFix(os.Args)),
				emptyChildrenWarning(parentKey, len(views), remoteRerunFix(os.Args))...)
			outputScoped(ctx, cmd, cmdutil.WithWarnings(result, warns...))
			return nil
		},
	}
}

// runChildrenRemote answers `item children --remote` from the Web API, which
// is the only plane that can see children written since Zotero desktop last
// synced — including the ones this CLI wrote seconds ago.
func runChildrenRemote(ctx context.Context, cmd *cli.Command, parentKey string) error {
	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	children, err := c.ListChildren(ctx, parentKey)
	if err != nil {
		return err
	}
	views := lo.Map(children, apiChildItemView)
	outputScoped(ctx, cmd, zot.ChildrenListResult{
		ParentKey: parentKey,
		Count:     len(views),
		Children:  views,
	})
	return nil
}

// emptyChildrenWarning flags the one answer a local read cannot be trusted
// to give. Nothing in a zero-children payload distinguishes "this parent is
// genuinely childless" from "the mirror predates the child we are asking
// about" — and the second is the normal state right after `item attach` or
// `content extract`, since desktop sync-back is minutes-slow. A caller that
// reads the confident zero as permission to write attaches a duplicate.
//
// Deliberately scoped to the zero case: a non-empty listing may still be
// short a child, but that is the ordinary staleness localReadWarnings
// already reports. Zero is where the ambiguity changes what a caller does.
func emptyChildrenWarning(parentKey string, count int, fix string) []cmdutil.Warning {
	if count > 0 {
		return nil
	}
	return []cmdutil.Warning{{
		Code: cmdutil.CodeStaleLocal,
		Message: fmt.Sprintf(
			"%s has no children in the local mirror, which cannot distinguish a childless item "+
				"from one whose children were written since the last Zotero desktop sync — "+
				"do not treat this as permission to write", parentKey),
		Fix: fix,
	}}
}

// toChildItemView projects a local.ChildItem into the zot-package
// mirror type used by ChildrenListResult. The duplication exists to
// break the local → zot import cycle; see zot.ChildItemView's doc.
func toChildItemView(ch local.ChildItem, _ int) zot.ChildItemView {
	return zot.ChildItemView{
		Key:         ch.Key,
		ItemType:    ch.ItemType,
		Title:       ch.Title,
		Note:        ch.Note,
		ContentType: ch.ContentType,
		Filename:    ch.Filename,
		Md5:         ch.Md5,
		Tags:        ch.Tags,
	}
}

// apiChildItemView is toChildItemView's --remote twin. The two shapes are
// field-identical by design: which plane answered must not change the
// payload a caller parses.
func apiChildItemView(ch api.ChildItem, _ int) zot.ChildItemView {
	return zot.ChildItemView{
		Key:         ch.Key,
		ItemType:    ch.ItemType,
		Title:       ch.Title,
		Note:        ch.Note,
		ContentType: ch.ContentType,
		Filename:    ch.Filename,
		Md5:         ch.Md5,
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

// localSearchScope describes which fields a local search matched against,
// shown on empty results so users know what was (and wasn't) searched.
func localSearchScope(contentSearch bool) string {
	scope := "title, DOI, publication, creators, citekey"
	if contentSearch {
		scope += " + paper full text"
	}
	return scope + " (local)"
}

// localSearchHint suggests the next-wider search when a local query comes
// back empty.
func localSearchHint(contentSearch bool) string {
	var wider []string
	if !contentSearch {
		wider = append(wider, "--content to also match the text of your papers")
	}
	wider = append(wider, "--remote for the Zotero Web index (abstract + notes + PDFs)")
	return "try " + strings.Join(wider, ", or ")
}

// codeContentStale labels the stale-content-index warning.
const codeContentStale cmdutil.Code = "content-stale"

// staleContentMessage explains a stale index in terms of what the user
// would have to do differently. The two causes are not the same story:
// one means their library moved, the other means sci's own indexing
// changed and nothing they did is wrong.
func staleContentMessage(reason content.StaleReason) string {
	switch reason {
	case content.StaleFormat:
		return "the content index was built by an older version of sci — its text still " +
			"includes each extraction's provenance header, which skews ranking and snippets"
	default:
		return "the content index is out of date — papers extracted since the last " +
			"build are not searchable"
	}
}

// contentSearch is the search command's handle on the paper-text index:
// the widening hook [local.SearchOptions] calls while searching, the
// snippet lookup for the hits that survive, and the warnings and closer
// that come with holding the index open.
type contentSearch struct {
	widen    func(text string) (map[string]float64, error)
	snippets func(query string, keys []string) map[string]string
	warns    []cmdutil.Warning
	close    func()
}

// contentWidener opens the content index and wires up [contentSearch].
//
// A missing index is an error with a runnable fix rather than a silent
// build: indexing a real library takes about a minute, which is not
// something to spring on someone who typed a search.
func contentWidener(ctx context.Context, db local.Reader) (*contentSearch, error) {
	ix, err := openContentIndex(db)
	if err != nil {
		return nil, err
	}
	closer := func() { _ = ix.Close() }

	st, err := ix.Stats()
	if err != nil {
		closer()
		return nil, err
	}
	if st.Total == 0 {
		closer()
		return nil, cmdutil.Coded(cmdutil.CodeNotFound,
			"no content index for this library").
			WithFix("sci zot content build --library " + scopeFromCtx(ctx))
	}

	var warns []cmdutil.Warning
	reason, err := content.Stale(ix, db)
	if err != nil {
		closer()
		return nil, err
	}
	if reason != content.StaleFresh {
		warns = append(warns, cmdutil.Warning{
			Code:    codeContentStale,
			Message: staleContentMessage(reason),
			Fix:     "sci zot content build --library " + scopeFromCtx(ctx),
		})
	}

	widen := func(text string) (map[string]float64, error) {
		scores, err := ix.Scores(text)
		if err != nil {
			// A query with no indexable terms (all punctuation) widens
			// nothing; the metadata clauses still stand on their own.
			if errors.Is(err, content.ErrNoTerms) {
				return nil, nil
			}
			return nil, err
		}
		return scores, nil
	}
	// Snippets are cosmetic: a hit list that loses them is still correct,
	// so a failed excerpt lookup drops the excerpts instead of the search.
	snippets := func(query string, keys []string) map[string]string {
		text := local.QueryFreeText(query)
		if text == "" || len(keys) == 0 {
			return nil
		}
		snips, err := ix.Snippets(text, keys)
		if err != nil {
			return nil
		}
		return snips
	}
	return &contentSearch{widen: widen, snippets: snippets, warns: warns, close: closer}, nil
}

// dropTitleEchoes removes excerpts that only restate the hit's own title.
//
// The snippet line is there to add evidence beyond what the reader can
// already see. When a query matches on the title, the highest-scoring span in
// the body is usually the title again (docling opens each extraction with it
// as a heading), so the line costs a row and returns nothing. Dropping it is
// safe: [zot.ListResult] and [zot.ListBriefResult] both render snippets by
// map lookup, and a missing key renders no line at all.
func dropTitleEchoes(snippets map[string]string, items []local.Item) map[string]string {
	if len(snippets) == 0 {
		return snippets
	}
	titles := lo.SliceToMap(items, func(it local.Item) (string, string) {
		return it.Key, it.Title
	})
	return lo.OmitBy(snippets, func(key, snippet string) bool {
		return content.EchoesTitle(snippet, titles[key])
	})
}

// retiredSearchFlagError turns a removed flag into a message that names
// its replacement. --fulltext (Zotero's PDF word index) and --note-text
// (a substring scan over note bodies) were two half-answers to one
// question; --content is the whole answer, and consults both sources
// through a single index.
func retiredSearchFlagError() error {
	retired, replacement := "", "--content"
	switch {
	case searchFulltext:
		retired = "--fulltext"
	case searchNoteText:
		retired = "--note-text"
	default:
		return nil
	}
	err := cmdutil.Coded(cmdutil.CodeUsage,
		"%s has been replaced by %s", retired, replacement).
		WithTry("--content searches the full text of your papers (docling extractions, " +
			"falling back to Zotero's own text); build it once with `sci zot content build`")
	if fix := rewriteFlagFix(os.Args, retired, replacement); fix != "" {
		err = err.WithFix(fix)
	}
	return err
}

// rewriteFlagFix rebuilds the user's command line with one flag swapped
// for another, so a retired flag yields a Fix they can resubmit verbatim.
//
// Returns "" when argv is not a recognizable `sci … zot …` invocation —
// under `go test` os.Args belongs to the test binary, and a Fix must be
// a real command or nothing at all.
func rewriteFlagFix(argv []string, retired, replacement string) string {
	if len(argv) < 2 || !lo.Contains(argv[1:], "zot") {
		return ""
	}
	parts := lo.Map(argv[1:], func(arg string, _ int) string {
		if arg == retired {
			return replacement
		}
		return shellQuote(arg)
	})
	return "sci " + strings.Join(parts, " ")
}
