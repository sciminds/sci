package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/enrich"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// collAddStdin is the stdin source for `zot collection add` when the user
// passes `-` or `--from-file -`. Overridable by tests.
var collAddStdin io.Reader = os.Stdin

// Write-command flag destinations (package-scoped, matching sci-go conventions).
var (
	addType        string
	addTitle       string
	addDOI         string
	addURL         string
	addDate        string
	addAbstract    string
	addPublication string
	addAuthor      []string
	addCollection  string
	addTag         []string
	addExtra       string
	addOpenAlex    string

	updTitle       string
	updDOI         string
	updURL         string
	updDate        string
	updAbstract    string
	updPublication string
	updExtra       string

	deleteYes bool

	collNewParent   string
	collAddFromFile string

	tagRemoveYes bool
	tagDeleteYes bool
)

// requireAPIClient builds an API client from the loaded config, short-circuiting
// if the machine is offline or not configured. The library scope stashed on ctx
// by ValidateLibraryBefore routes the client to personal or shared endpoints.
func requireAPIClient(ctx context.Context) (*api.Client, error) {
	cfg, err := zot.RequireConfig()
	if err != nil {
		return nil, err
	}
	if !netutil.Online() {
		return nil, fmt.Errorf("no internet connection — zot writes require network access")
	}
	ref, err := resolveLibraryRef(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return api.New(cfg, api.WithLibrary(ref))
}

// resolveLibraryRef upgrades the scope ctx carries (from ValidateLibraryBefore)
// into a full zot.LibraryRef by reading config. Shared-scope resolution uses
// ResolveWithProbe so a first-time --library shared command lazily auto-detects
// the group via the Web API and caches the result. Errors if the ctx was not
// seeded — every zot command except `setup` must go through
// ValidateLibraryBefore, so a missing ref indicates a wiring bug.
func resolveLibraryRef(ctx context.Context, cfg *zot.Config) (zot.LibraryRef, error) {
	partial, ok := LibraryFromContext(ctx)
	if !ok {
		return zot.LibraryRef{}, fmt.Errorf("library scope not found in context — did you pass --library?")
	}
	probe := func(_, _ string) ([]zot.GroupRef, error) {
		// Build a throwaway client bound to the personal library just to
		// call ListGroups. Only invoked when shared-scope config is blank.
		tmp, err := api.New(cfg, api.WithLibrary(zot.LibraryRef{
			Scope:   zot.LibPersonal,
			APIPath: "users/" + cfg.UserID,
		}))
		if err != nil {
			return nil, err
		}
		return tmp.ListGroups(ctx)
	}
	return cfg.ResolveWithProbe(partial.Scope, probe)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func addCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Create a new item in your Zotero library",
		Description: "$ zot item add --type journalArticle --title \"My Paper\" --author \"Smith, Alice\" --doi 10.1000/abc\n" +
			"$ zot item add --openalex 10.1038/nature12373\n" +
			"$ zot item add --openalex W2963403868 --collection ABC12345 --tag ml",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "openalex", Usage: "lookup metadata on OpenAlex by DOI / W…-ID / arXiv / PMID", Destination: &addOpenAlex, Local: true},
			&cli.StringFlag{Name: "type", Value: "journalArticle", Usage: "item type (e.g. journalArticle, book, webpage)", Destination: &addType, Local: true},
			&cli.StringFlag{Name: "title", Usage: "item title (required unless --openalex)", Destination: &addTitle, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "DOI (no URL prefix)", Destination: &addDOI, Local: true},
			&cli.StringFlag{Name: "url", Usage: "URL", Destination: &addURL, Local: true},
			&cli.StringFlag{Name: "date", Usage: "publication date (freeform)", Destination: &addDate, Local: true},
			&cli.StringFlag{Name: "abstract", Usage: "abstract / summary", Destination: &addAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "journal / publication title", Destination: &addPublication, Local: true},
			&cli.StringSliceFlag{Name: "author", Usage: "author as \"Last, First\" (repeatable)", Destination: &addAuthor, Local: true},
			&cli.StringFlag{Name: "collection", Usage: "add item to collection key", Destination: &addCollection, Local: true},
			&cli.StringSliceFlag{Name: "tag", Usage: "attach a tag (repeatable)", Destination: &addTag, Local: true},
			&cli.StringFlag{Name: "extra", Usage: "free-text extra field (key: value lines)", Destination: &addExtra, Local: true},
		},
		Action: runAdd,
	}
}

func runAdd(ctx context.Context, cmd *cli.Command) error {
	data, err := buildAddItemData(ctx)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	key, err := c.CreateItem(ctx, data)
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, zot.WriteResult{Action: "created", Kind: "item", Target: key})
	return nil
}

// buildAddItemData composes the ItemData payload for `zot item add`. The
// --openalex path fetches + maps metadata, then manual flags overlay the
// result (so "--openalex W… --tag ml --collection XYZ" works as expected).
func buildAddItemData(ctx context.Context) (client.ItemData, error) {
	var data client.ItemData
	if addOpenAlex != "" {
		oa, err := openalexClient()
		if err != nil {
			return data, err
		}
		work, err := oa.ResolveWork(ctx, addOpenAlex)
		if err != nil {
			return data, fmt.Errorf("openalex lookup: %w", err)
		}
		data = enrich.ToItemFields(work)
	} else {
		if addTitle == "" {
			return data, fmt.Errorf("--title is required")
		}
		data = client.ItemData{
			ItemType: client.ItemDataItemType(addType),
			Title:    &addTitle,
		}
	}

	applyAddFlagOverrides(&data)
	return data, nil
}

// applyAddFlagOverrides lets explicit flags override any field already set by
// the --openalex mapping. Empty flags leave the mapped value untouched.
func applyAddFlagOverrides(data *client.ItemData) {
	if addType != "" && addType != "journalArticle" {
		// Only override itemType when the user explicitly changed it from the
		// default — otherwise --openalex's inference wins.
		data.ItemType = client.ItemDataItemType(addType)
	}
	if addTitle != "" {
		data.Title = &addTitle
	}
	if addDOI != "" {
		data.DOI = &addDOI
	}
	if addURL != "" {
		data.Url = &addURL
	}
	if addDate != "" {
		data.Date = &addDate
	}
	if addAbstract != "" {
		data.AbstractNote = &addAbstract
	}
	if addPublication != "" {
		data.PublicationTitle = &addPublication
	}
	if addExtra != "" {
		data.Extra = &addExtra
	}
	if len(addAuthor) > 0 {
		creators := lo.Map(addAuthor, func(a string, _ int) client.Creator { return parseCreator(a) })
		data.Creators = &creators
	}
	if addCollection != "" {
		colls := []string{addCollection}
		data.Collections = &colls
	}
	if len(addTag) > 0 {
		tags := lo.Map(addTag, func(t string, _ int) client.Tag { return client.Tag{Tag: t} })
		data.Tags = &tags
	}
}

// parseCreator parses a "Last, First" string into a client.Creator. Inputs
// without a comma are treated as single-name creators (institutions).
func parseCreator(s string) client.Creator {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			last := trim(s[:i])
			first := trim(s[i+1:])
			return client.Creator{CreatorType: "author", FirstName: &first, LastName: &last}
		}
	}
	name := trim(s)
	return client.Creator{CreatorType: "author", Name: &name}
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update fields on one or more items",
		Description: "$ zot item update ABC12345 --title \"Corrected Title\"\n" +
			"$ zot item update ABC12345 DEF67890 --publication \"Nature\"\n" +
			"Providing multiple keys applies the same field patch to each item via a\n" +
			"batched POST /items request (up to 50 items per round-trip).",
		ArgsUsage: "<key> [<key>...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Destination: &updTitle, Local: true},
			&cli.StringFlag{Name: "doi", Destination: &updDOI, Local: true},
			&cli.StringFlag{Name: "url", Destination: &updURL, Local: true},
			&cli.StringFlag{Name: "date", Destination: &updDate, Local: true},
			&cli.StringFlag{Name: "abstract", Destination: &updAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Destination: &updPublication, Local: true},
			&cli.StringFlag{Name: "extra", Destination: &updExtra, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keys := cmd.Args().Slice()
			if len(keys) == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least one item key")
			}

			patch := client.ItemData{}
			anyField := false
			set := func(dst **string, v string) {
				if v != "" {
					*dst = strPtr(v)
					anyField = true
				}
			}
			set(&patch.Title, updTitle)
			set(&patch.DOI, updDOI)
			set(&patch.Url, updURL)
			set(&patch.Date, updDate)
			set(&patch.AbstractNote, updAbstract)
			set(&patch.PublicationTitle, updPublication)
			set(&patch.Extra, updExtra)
			if !anyField {
				return cmdutil.UsageErrorf(cmd, "at least one field flag is required")
			}

			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}

			if len(keys) == 1 {
				// Fast path: single PATCH. UpdateItem fills in
				// ItemType internally if not supplied.
				if err := c.UpdateItem(ctx, keys[0], patch); err != nil {
					return err
				}
				cmdutil.Output(cmd, zot.WriteResult{Action: "updated", Kind: "item", Target: keys[0]})
				return nil
			}

			patches := lo.Map(keys, func(k string, _ int) api.ItemPatch {
				return api.ItemPatch{Key: k, Data: patch}
			})
			results, err := c.UpdateItemsBatch(ctx, patches)
			if err != nil {
				return err
			}
			var success []string
			failed := map[string]string{}
			for _, k := range keys {
				if e := results[k]; e != nil {
					failed[k] = e.Error()
				} else {
					success = append(success, k)
				}
			}
			cmdutil.Output(cmd, zot.BulkWriteResult{
				Action:  "updated",
				Kind:    "item",
				Total:   len(keys),
				Success: success,
				Failed:  failed,
			})
			return nil
		},
	}
}

func deleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Aliases:     []string{"trash"},
		Usage:       "Move an item to trash",
		Description: "$ zot item delete ABC12345\n$ zot item delete ABC12345 --yes",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &deleteYes, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			if done, err := cmdutil.ConfirmOrSkip(deleteYes, fmt.Sprintf("Move item %s to trash?", key)); done || err != nil {
				return err
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			if err := c.TrashItem(ctx, key); err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.WriteResult{
				Action: "trashed",
				Kind:   "item",
				Target: key,
			})
			return nil
		},
	}
}

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:        "collection",
		Aliases:     []string{"coll"},
		Usage:       "Manage collections (list, create, delete, add/remove items)",
		Description: "$ zot collection list\n$ zot collection create \"Brain Papers\"\n$ zot collection add ABC12345 COLLXXX1\n$ zot collection delete COLLXXX1",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every collection in the library with item counts",
				Description: "$ zot collection list",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					_, db, err := openLocalDB(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					colls, err := db.ListCollections()
					if err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
					return nil
				},
			},
			{
				Name:        "create",
				Usage:       "Create a new collection",
				Description: "$ zot collection create \"Brain Papers\"\n$ zot collection create \"Sub-topic\" --parent COLLXXX1",
				ArgsUsage:   "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "parent", Usage: "parent collection key", Destination: &collNewParent, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a collection name")
					}
					name := cmd.Args().First()
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					key, err := c.CreateCollection(ctx, name, collNewParent)
					if err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{Action: "created", Kind: "collection", Target: key,
						Message: fmt.Sprintf("created collection %q (%s)", name, key)})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a collection",
				Description: "$ zot collection delete COLLXXX1\n$ zot collection delete COLLXXX1 --yes",
				ArgsUsage:   "<key>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &deleteYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a collection key")
					}
					key := cmd.Args().First()
					if done, err := cmdutil.ConfirmOrSkip(deleteYes, fmt.Sprintf("Delete collection %s?", key)); done || err != nil {
						return err
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.DeleteCollection(ctx, key); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{Action: "deleted", Kind: "collection", Target: key})
					return nil
				},
			},
			{
				Name:  "add",
				Usage: "Add one or many items to a collection",
				Description: "$ zot collection add ABC12345 COLLXXX1\n" +
					"$ zot collection add --from-file keys.txt COLLXXX1\n" +
					"$ cat keys.txt | zot collection add - COLLXXX1",
				ArgsUsage: "<itemKey> <collectionKey>  (or --from-file FILE <collectionKey>; '-' reads stdin)",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "from-file",
						Usage:       "read item keys from file (one per line, '#' comments); '-' reads stdin",
						Destination: &collAddFromFile,
						Local:       true,
					},
				},
				Action: runCollectionAdd,
			},
			{
				Name:        "remove",
				Usage:       "Remove an item from a collection",
				Description: "$ zot collection remove ABC12345 COLLXXX1",
				ArgsUsage:   "<itemKey> <collectionKey>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <collectionKey>")
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.RemoveItemFromCollection(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "removed", Kind: "item", Target: args[0],
						Message: fmt.Sprintf("removed item %s from collection %s", args[0], args[1]),
					})
					return nil
				},
			},
			collBrowseCommand(),
		},
	}
}

func tagsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tags",
		Aliases:     []string{"tag"},
		Usage:       "Manage tags (list, add/remove per item, delete library-wide)",
		Description: "$ zot tags list\n$ zot tags add ABC12345 neuroimaging\n$ zot tags remove ABC12345 deprecated\n$ zot tags delete deprecated",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every tag in the library with usage counts",
				Description: "$ zot tags list",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					_, db, err := openLocalDB(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					tags, err := db.ListTags()
					if err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.TagListResult{Count: len(tags), Tags: tags})
					return nil
				},
			},
			{
				Name:        "add",
				Usage:       "Attach a tag to an item",
				Description: "$ zot tags add ABC12345 neuroimaging",
				ArgsUsage:   "<itemKey> <tag>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.AddTagToItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "added", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("added tag %q to item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "remove",
				Usage:       "Remove a tag from a single item",
				Description: "$ zot tags remove ABC12345 deprecated\n$ zot tags remove ABC12345 deprecated --yes",
				ArgsUsage:   "<itemKey> <tag>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Destination: &tagRemoveYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					if done, err := cmdutil.ConfirmOrSkip(tagRemoveYes,
						fmt.Sprintf("Remove tag %q from item %s?", args[1], args[0])); done || err != nil {
						return err
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.RemoveTagFromItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "removed", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("removed tag %q from item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a tag from ALL items in the library",
				Description: "$ zot tags delete deprecated\n$ zot tags delete deprecated --yes\nRemoves the tag from every item in the library in one API call.",
				ArgsUsage:   "<tag>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Destination: &tagDeleteYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a tag name")
					}
					tag := cmd.Args().First()
					if done, err := cmdutil.ConfirmOrSkip(tagDeleteYes,
						fmt.Sprintf("Delete tag %q from ALL items in the library?", tag)); done || err != nil {
						return err
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.DeleteTagsFromLibrary(ctx, []string{tag}); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "deleted", Kind: "tag", Target: tag,
						Message: fmt.Sprintf("deleted tag %q from library", tag),
					})
					return nil
				},
			},
			tagsBrowseCommand(),
		},
	}
}

// runCollectionAdd handles both the single-item fast path and the bulk
// (--from-file / stdin) path. When many keys are supplied, we read the
// current collections + Version + ItemType from the local DB so
// UpdateItemsBatch can skip per-item GETs — a 2145-item run becomes ~43
// HTTP POSTs (batches of 50) instead of 4290 round-trips.
func runCollectionAdd(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	keys, collKey, err := resolveCollectionAddKeys(args, collAddFromFile, collAddStdin)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	// Single-item fast path: preserve the original <itemKey> <collectionKey>
	// shape so callers and scripts that use it keep working.
	if len(keys) == 1 && collAddFromFile == "" && args[0] != "-" {
		if err := c.AddItemToCollection(ctx, keys[0], collKey); err != nil {
			return err
		}
		cmdutil.Output(cmd, zot.WriteResult{
			Action: "added", Kind: "item", Target: keys[0],
			Message: fmt.Sprintf("added item %s to collection %s", keys[0], collKey),
		})
		return nil
	}

	// Bulk path: load local snapshots for every requested key in one SQL
	// round-trip, merge collKey into each Item's Collections, batch-POST.
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	items, err := db.GetItemsByKeys(keys)
	if err != nil {
		return err
	}

	patches, alreadyMember := buildCollectionAddPatches(items, collKey)

	// Keys the local DB didn't return → not found in scope (trashed, wrong
	// library, typo). Surface but don't abort: the batch still applies to
	// the keys we did find.
	found := lo.Keyify(lo.Map(items, func(it local.Item, _ int) string { return it.Key }))
	result := zot.BulkWriteResult{
		Action:  "added",
		Kind:    "item",
		Total:   len(keys),
		Success: slices.Clone(alreadyMember),
		Failed:  map[string]string{},
	}
	for _, k := range keys {
		if _, ok := found[k]; !ok {
			result.Failed[k] = "not found in local library"
		}
	}

	if len(patches) > 0 {
		apiResults, err := c.UpdateItemsBatch(ctx, patches)
		if err != nil {
			return err
		}
		for _, p := range patches {
			if e := apiResults[p.Key]; e != nil {
				result.Failed[p.Key] = e.Error()
			} else {
				result.Success = append(result.Success, p.Key)
			}
		}
	}

	cmdutil.Output(cmd, result)
	return nil
}

// resolveCollectionAddKeys decodes the argv shape into (itemKeys, collKey).
// Rules:
//   - 2 positionals, first != "-": single-item fast path, collKey = arg[1].
//   - 1 positional + --from-file: keys come from file (or stdin if path is "-").
//   - 2 positionals, first == "-": keys come from stdin, collKey = arg[1].
//   - mixing --from-file with a leading key positional is a usage error.
//   - empty input (after normalization) is a usage error.
func resolveCollectionAddKeys(args []string, fromFile string, stdin io.Reader) (keys []string, collKey string, err error) {
	switch {
	case fromFile != "" && len(args) == 1:
		collKey = args[0]
		src, closer, serr := openKeySource(fromFile, stdin)
		if serr != nil {
			return nil, "", serr
		}
		defer closer()
		keys, err = parseKeysFromReader(src)
	case fromFile != "" && len(args) != 1:
		return nil, "", fmt.Errorf("pass a single <collectionKey> positional when using --from-file (got %d)", len(args))
	case len(args) == 2 && args[0] == "-":
		collKey = args[1]
		keys, err = parseKeysFromReader(stdin)
	case len(args) == 2:
		return []string{args[0]}, args[1], nil
	default:
		return nil, "", fmt.Errorf("expected <itemKey> <collectionKey>, or --from-file FILE <collectionKey>")
	}
	if err != nil {
		return nil, "", err
	}
	if len(keys) == 0 {
		return nil, "", fmt.Errorf("no item keys provided")
	}
	return keys, collKey, nil
}

// openKeySource returns a reader for the requested file, or stdin if path
// is "-". The caller must invoke closer() when done.
func openKeySource(path string, stdin io.Reader) (io.Reader, func(), error) {
	if path == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %q: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

// parseKeysFromReader reads item keys one per line, trimming whitespace,
// skipping blank lines and '#'-prefixed comments, and de-duplicating while
// preserving first-seen order. Suitable for piped doctor output and
// hand-edited lists alike.
func parseKeysFromReader(r io.Reader) ([]string, error) {
	var (
		out  []string
		seen = map[string]struct{}{}
		sc   = bufio.NewScanner(r)
	)
	// Zotero keys are 8 chars, but some pipelines might feed longer lines
	// (whole JSON records) — bump the buffer to avoid scanner truncation.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildCollectionAddPatches splits local items into (needs-update, already-member).
// Items already in collKey produce no patch (zero API cost); the rest get a
// patch carrying Version + ItemType so UpdateItemsBatch's fast path avoids
// per-item GETs.
func buildCollectionAddPatches(items []local.Item, collKey string) (patches []api.ItemPatch, alreadyMember []string) {
	for _, it := range items {
		if slices.Contains(it.Collections, collKey) {
			alreadyMember = append(alreadyMember, it.Key)
			continue
		}
		merged := append(slices.Clone(it.Collections), collKey)
		patches = append(patches, api.ItemPatch{
			Key:      it.Key,
			Version:  it.Version,
			ItemType: it.Type,
			Data:     client.ItemData{Collections: &merged},
		})
	}
	return patches, alreadyMember
}
