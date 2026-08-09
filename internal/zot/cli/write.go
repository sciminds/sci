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
	"github.com/sciminds/cli/internal/zot/backfill"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/enrich"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/pkg/citekey"
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
	updFromJSON    string

	deleteYes bool

	collNewParent   string
	collAddFromFile string
	collListRemote  bool

	tagRemoveYes bool
	tagDeleteYes bool
)

// requireAPIClient builds an API client from the loaded config, short-circuiting
// if the machine is offline or not configured. The library scope is resolved
// via ensureLibraryScope (auto-select / prompt / error per the holder set up
// by ValidateLibraryBefore).
func requireAPIClient(ctx context.Context) (*api.Client, error) {
	cfg, err := requireConfigCoded()
	if err != nil {
		return nil, err
	}
	if !netutil.Online() {
		return nil, cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — zot writes require network access").
			WithTry("re-run when online; local reads (search, item read/list, bib, export) work offline")
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if ref.Scope == zot.LibAll {
		return nil, cmdutil.Coded(cmdutil.CodeUsage,
			"--library all is local-read-only — API operations target one library").
			WithTry("re-run with --library personal or --library shared")
	}
	return api.New(cfg, api.WithLibrary(ref))
}

// buildItemPatch turns the --field flags into a PATCH body.
//
// Presence is read from cmd.IsSet, not from the value, because "leave this
// alone" and "empty this" are different instructions and a bare string
// cannot tell them apart. Before this there was no way to clear a field
// through the CLI at all, which surfaced on two items whose Extra held
// nothing but a `DOI-source:` line that a merge had made untrue.
//
// It deliberately does NOT use strPtr: that helper returns nil for an empty
// string, which is right for "the caller said nothing" and exactly wrong
// here. Going through it turned `--extra ""` into a patch carrying only
// key/version/itemType — a write that reports success and changes nothing,
// the same shape as the 709 empty patches that once reported
// "applied 709 of 709".
//
// --publication comes back SEPARATELY rather than on the patch, because its
// field name depends on the item's type (see applyVenue) and one patch may
// be applied to many keys of different types. `any` still counts it, so
// `--publication` alone is a valid edit.
func buildItemPatch(cmd *cli.Command) (patch client.ItemData, venue *string, any bool) {
	set := func(dst **string, flag string) {
		if !cmd.IsSet(flag) {
			return
		}
		v := cmd.String(flag)
		*dst = &v
		any = true
	}
	set(&patch.Title, "title")
	set(&patch.DOI, "doi")
	set(&patch.Url, "url")
	set(&patch.Date, "date")
	set(&patch.AbstractNote, "abstract")
	set(&venue, "publication")
	set(&patch.Extra, "extra")
	return patch, venue, any
}

// venueResolver is the slice of *api.Client that venue routing needs: given
// an item type, which field does Zotero's template say carries its venue.
type venueResolver interface {
	VenueField(ctx context.Context, itemType string) (string, error)
}

// venueTargeter adds the per-item type lookup `item update` needs on top of
// venueResolver — the same patch may be applied to a journal article and a
// book chapter in one call.
type venueTargeter interface {
	venueResolver
	GetItem(ctx context.Context, key string) (*client.Item, error)
}

// venuePatches returns the patch to send for each key, identical to the
// shared one unless --publication was passed — in which case each key's
// venue lands in whatever field ITS type declares.
//
// The extra GET per key is only paid when --publication is set, and it is
// the item's own type that decides the field: guessing from the first key
// would write a bookTitle onto a journal article and fail that whole item.
func venuePatches(ctx context.Context, c venueTargeter, keys []string, patch client.ItemData, venue *string) (map[string]client.ItemData, error) {
	out := lo.SliceToMap(keys, func(k string) (string, client.ItemData) { return k, patch })
	if venue == nil {
		return out, nil
	}
	for _, k := range keys {
		it, err := c.GetItem(ctx, k)
		if err != nil {
			return nil, err
		}
		data := patch
		if err := applyVenue(ctx, c, &data, string(it.Data.ItemType), *venue); err != nil {
			return nil, err
		}
		out[k] = data
	}
	return out, nil
}

// applyVenue places a --publication value in the field the item type
// actually declares — publicationTitle for a journal article, bookTitle for
// a book chapter, proceedingsTitle for a conference paper.
//
// Sending the wrong one is not a degraded write, it is a failed one:
// Zotero answers "'publicationTitle' is not a valid field for type
// 'bookSection'" and the whole request dies. A type with no venue field at
// all is bad input, so it is a usage error (exit 2) rather than a runtime
// error surfaced from the API after a round trip.
func applyVenue(ctx context.Context, r venueResolver, data *client.ItemData, itemType, value string) error {
	field, err := r.VenueField(ctx, itemType)
	if err != nil {
		return err
	}
	if field == "" {
		return cmdutil.Coded(cmdutil.CodeUsage,
			"--publication is not a valid field for item type %q", itemType).
			WithTry("only types with a container take one — use --type bookSection for a chapter in an edited volume, or --type conferencePaper for a proceedings paper")
	}
	return api.SetVenueField(data, field, value)
}

func addCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Create a new item in your Zotero library",
		Description: "$ sci zot item add --type journalArticle --title \"My Paper\" --author \"Smith, Alice\" --doi 10.1000/abc\n" +
			"$ sci zot item add --type bookSection --title \"A Chapter\" --publication \"The Edited Volume\"\n" +
			"$ sci zot item add --openalex 10.1038/nature12373\n" +
			"$ sci zot item add --openalex W2963403868 --collection ABC12345 --tag ml\n\n" +
			"--publication carries the VENUE, and Zotero names that field per item\n" +
			"type: publicationTitle on a journal article, bookTitle on a bookSection,\n" +
			"proceedingsTitle on a conferencePaper. The right one is chosen from the\n" +
			"type's own Zotero template; a type with no venue field says so.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "openalex", Usage: "lookup metadata on OpenAlex by DOI / W…-ID / arXiv / PMID", Destination: &addOpenAlex, Local: true},
			&cli.StringFlag{Name: "type", Value: "journalArticle", Usage: "item type (e.g. journalArticle, book, webpage)", Destination: &addType, Local: true},
			&cli.StringFlag{Name: "title", Usage: "item title (required unless --openalex)", Destination: &addTitle, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "DOI (no URL prefix)", Destination: &addDOI, Local: true},
			&cli.StringFlag{Name: "url", Usage: "URL", Destination: &addURL, Local: true},
			&cli.StringFlag{Name: "date", Usage: "publication date (freeform)", Destination: &addDate, Local: true},
			&cli.StringFlag{Name: "abstract", Usage: "abstract / summary", Destination: &addAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "venue title — routed to the field the item type takes (publicationTitle / bookTitle / proceedingsTitle)", Destination: &addPublication, Local: true},
			&cli.StringSliceFlag{Name: "author", Usage: "author as \"Last, First\" (repeatable)", Destination: &addAuthor}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringFlag{Name: "collection", Usage: "add item to collection key", Destination: &addCollection, Local: true},
			&cli.StringSliceFlag{Name: "tag", Usage: "attach a tag (repeatable)", Destination: &addTag}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
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
	// Resolved after the client exists because the field name comes from
	// Zotero's own template for the type. The type is whatever survived
	// applyAddFlagOverrides — --type when given, else --openalex's guess.
	if addPublication != "" {
		if err := applyVenue(ctx, c, &data, string(data.ItemType), addPublication); err != nil {
			return err
		}
	}
	it, err := c.CreateItem(ctx, data)
	if err != nil {
		return err
	}
	hydrated := api.ItemFromClient(it)
	citekey.Enrich(&hydrated)
	outputScoped(ctx, cmd, zot.WriteResult{
		Action: "created",
		Kind:   "item",
		Target: it.Key,
		Data:   hydrated,
	})
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
	// --publication is deliberately NOT applied here: its field name
	// depends on the item type, which this function is still deciding.
	// runAdd places it once the type is settled (see applyVenue).
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
		Description: "$ sci zot item update ABC12345 --title \"Corrected Title\"\n" +
			"$ sci zot item update ABC12345 DEF67890 --publication \"Nature\"\n" +
			"$ sci zot item update --from-json doi-backfill.ndjson\n" +
			"$ sci zot item update --from-json enrich-plan.ndjson\n" +
			"Providing multiple keys applies the same field patch to each item via a\n" +
			"batched POST /items request (up to 50 items per round-trip).\n\n" +
			"--from-json applies MANY DISTINCT patches instead of one patch to many\n" +
			"keys. It reads either plan the zot binary writes: a DOI plan from `zot\n" +
			"backfill`, or a field plan from `zot enrich` (abstracts, volume, issue,\n" +
			"pages, PMID). Rows of both kinds may share one file.\n\n" +
			"Nothing is overwritten. A DOI plan composes Extra from the SERVER's copy\n" +
			"rather than the plan, so a note added on another device is not erased,\n" +
			"and an item that has gained a DOI since the plan was built is skipped. A\n" +
			"field plan checks EACH field against the server separately, so a volume\n" +
			"that appeared in the meantime costs that one field, not the other four.",
		ArgsUsage: "<key> [<key>...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Destination: &updTitle, Local: true},
			&cli.StringFlag{Name: "doi", Destination: &updDOI, Local: true},
			&cli.StringFlag{Name: "url", Destination: &updURL, Local: true},
			&cli.StringFlag{Name: "date", Destination: &updDate, Local: true},
			&cli.StringFlag{Name: "abstract", Destination: &updAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "venue title — routed per item to the field ITS type takes", Destination: &updPublication, Local: true},
			&cli.StringFlag{Name: "extra", Destination: &updExtra, Local: true},
			&cli.StringFlag{Name: "from-json", Destination: &updFromJSON, Local: true,
				Usage: "apply a patch plan (NDJSON from `zot backfill` or `zot enrich`)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keys := cmd.Args().Slice()
			if updFromJSON != "" {
				if len(keys) > 0 {
					return cmdutil.UsageErrorf(cmd,
						"--from-json carries its own keys; do not also pass them as arguments")
				}
				return runBackfillPlan(ctx, cmd)
			}
			if len(keys) == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least one item key")
			}

			patch, venue, anyField := buildItemPatch(cmd)
			if !anyField {
				return cmdutil.UsageErrorf(cmd, "at least one field flag is required")
			}

			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}

			// One patch, many keys — but a venue field name is per TYPE,
			// so --publication has to be placed per key.
			perKey, err := venuePatches(ctx, c, keys, patch, venue)
			if err != nil {
				return err
			}

			if len(keys) == 1 {
				// Fast path: single PATCH. UpdateItem fills in
				// ItemType internally if not supplied.
				if err := c.UpdateItem(ctx, keys[0], perKey[keys[0]]); err != nil {
					return err
				}
				outputScoped(ctx, cmd, zot.WriteResult{Action: "updated", Kind: "item", Target: keys[0]})
				return nil
			}

			patches := lo.Map(keys, func(k string, _ int) api.ItemPatch {
				return api.ItemPatch{Key: k, Data: perKey[k]}
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
			outputScoped(ctx, cmd, zot.BulkWriteResult{
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
		Description: "$ sci zot item delete ABC12345\n$ sci zot item delete ABC12345 --yes",
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
			outputScoped(ctx, cmd, zot.WriteResult{
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
		Description: "$ sci zot collection list\n$ sci zot collection create \"Brain Papers\"\n$ sci zot collection add ABC12345 COLLXXX1\n$ sci zot collection delete COLLXXX1",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every collection in the library with item counts",
				Description: "$ sci zot collection list\n$ sci zot collection list --remote   # bypass local SQLite, hit the Zotero Web API",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API (shows collections not yet synced locally)", Destination: &collListRemote, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if collListRemote {
						c, err := requireAPIClient(ctx)
						if err != nil {
							return err
						}
						raw, err := c.ListCollections(ctx)
						if err != nil {
							return err
						}
						colls := lo.Map(raw, func(c client.Collection, _ int) local.Collection {
							return api.CollectionFromClient(&c)
						})
						outputScoped(ctx, cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
						return nil
					}
					_, db, err := openLocalDB(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					colls, err := db.ListCollections()
					if err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
					return nil
				},
			},
			{
				Name:        "create",
				Usage:       "Create a new collection",
				Description: "$ sci zot collection create \"Brain Papers\"\n$ sci zot collection create \"Sub-topic\" --parent COLLXXX1",
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
					coll, err := c.CreateCollection(ctx, name, collNewParent)
					if err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action:  "created",
						Kind:    "collection",
						Target:  coll.Key,
						Message: fmt.Sprintf("created collection %q (%s)", name, coll.Key),
						Data:    api.CollectionFromClient(coll),
					})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a collection",
				Description: "$ sci zot collection delete COLLXXX1\n$ sci zot collection delete COLLXXX1 --yes",
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
					outputScoped(ctx, cmd, zot.WriteResult{Action: "deleted", Kind: "collection", Target: key})
					return nil
				},
			},
			{
				Name:  "add",
				Usage: "Add one or many items to a collection",
				Description: "$ sci zot collection add ABC12345 COLLXXX1\n" +
					"$ sci zot collection add --from-file keys.txt COLLXXX1\n" +
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
				Description: "$ sci zot collection remove ABC12345 COLLXXX1",
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
					outputScoped(ctx, cmd, zot.WriteResult{
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
		Description: "$ sci zot tags list\n$ sci zot tags add ABC12345 neuroimaging\n$ sci zot tags remove ABC12345 deprecated\n$ sci zot tags delete deprecated",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every tag in the library with usage counts",
				Description: "$ sci zot tags list",
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
					outputScoped(ctx, cmd, zot.TagListResult{Count: len(tags), Tags: tags})
					return nil
				},
			},
			{
				Name:        "add",
				Usage:       "Attach a tag to an item",
				Description: "$ sci zot tags add ABC12345 neuroimaging",
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
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "added", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("added tag %q to item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "remove",
				Usage:       "Remove a tag from a single item",
				Description: "$ sci zot tags remove ABC12345 deprecated\n$ sci zot tags remove ABC12345 deprecated --yes",
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
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "removed", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("removed tag %q from item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a tag from ALL items in the library",
				Description: "$ sci zot tags delete deprecated\n$ sci zot tags delete deprecated --yes\nRemoves the tag from every item in the library in one API call.",
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
					outputScoped(ctx, cmd, zot.WriteResult{
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
		outputScoped(ctx, cmd, zot.WriteResult{
			Action: "added", Kind: "item", Target: keys[0],
			Message: fmt.Sprintf("added item %s to collection %s", keys[0], collKey),
		})
		return nil
	}

	// Bulk path: load local snapshots for every requested key in one SQL
	// round-trip, merge collKey into each Item's Collections, batch-POST.
	// Any keys missing locally (common when the caller just created them
	// via the API and Zotero desktop hasn't synced yet) are fetched
	// individually from the Web API — correct at the cost of one GET per
	// miss; the common human case stays at zero API reads.
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	localItems, err := db.GetItemsByKeys(keys)
	if err != nil {
		return err
	}

	items, fallbackFailed := resolveBulkCollectionAddItems(
		keys, localItems,
		func(k string) (local.Item, error) {
			raw, gerr := c.GetItem(ctx, k)
			if gerr != nil {
				return local.Item{}, gerr
			}
			return api.ItemFromClient(raw), nil
		},
	)

	patches, alreadyMember := buildCollectionAddPatches(items, collKey)

	result := zot.BulkWriteResult{
		Action:  "added",
		Kind:    "item",
		Total:   len(keys),
		Success: slices.Clone(alreadyMember),
		Failed:  fallbackFailed,
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

	outputScoped(ctx, cmd, result)
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

// resolveBulkCollectionAddItems merges a local-DB snapshot with an API
// fallback, so `collection add --from-file` works for keys the local
// SQLite doesn't yet know about — typically items the same caller
// just created via the Web API before Zotero desktop synced them back.
//
// Local wins: keys already in `localItems` don't touch the API. Only
// keys missing from local are passed to getRemote (one call each —
// Zotero doesn't batch GETs by key across arbitrary keys without a
// server-side index we control). Per-key fetch errors land in `failed`
// so the caller can still POST a batch for the keys that did resolve.
func resolveBulkCollectionAddItems(
	keys []string,
	localItems []local.Item,
	getRemote func(key string) (local.Item, error),
) (items []local.Item, failed map[string]string) {
	failed = map[string]string{}
	have := lo.Keyify(lo.Map(localItems, func(it local.Item, _ int) string { return it.Key }))
	items = slices.Clone(localItems)
	for _, k := range keys {
		if _, ok := have[k]; ok {
			continue
		}
		it, err := getRemote(k)
		if err != nil {
			failed[k] = err.Error()
			continue
		}
		items = append(items, it)
	}
	return items, failed
}

// buildCollectionAddPatches splits local items into (needs-update, already-member).
// Items already in collKey produce no patch (zero API cost); the rest get a
// patch carrying Version + ItemType so UpdateItemsBatch's fast path avoids
// per-item GETs.
//
// The Version is not just a fast-path token — it is the safety interlock. The
// merged array is composed from the local mirror, which cannot see memberships
// added on the server since the last desktop sync, and a Zotero PATCH replaces
// `collections` wholesale. Submitting under the local version means any such
// membership makes the write 412 instead of silently erasing it; the Rebuild
// hook then re-runs the same union against the server's own array. Together
// they keep the zero-API-read fast path while making the stale case correct.
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
			Rebuild:  unionCollection(collKey),
		})
	}
	return patches, alreadyMember
}

// unionCollection returns the Rebuild hook for a collection-add patch: the
// same append, re-derived against whatever the server actually holds. Adding
// a collection the item already belongs to restates the server's array
// unchanged rather than duplicating the key, so a re-derive is idempotent.
func unionCollection(collKey string) func(*client.Item) (client.ItemData, error) {
	return func(cur *client.Item) (client.ItemData, error) {
		var current []string
		if cur.Data.Collections != nil {
			current = *cur.Data.Collections
		}
		merged := slices.Clone(current)
		if !slices.Contains(merged, collKey) {
			merged = append(merged, collKey)
		}
		return client.ItemData{Collections: &merged}, nil
	}
}

// runBackfillPlan applies a plan written by the zot binary — DOI rows from
// `zot backfill`, field rows from `zot enrich`, or both in one file.
//
// The plan is read and validated in full before a single write, so a bad
// row cannot leave the library half-patched in a state nobody planned.
//
// Rows are routed by the library each one names, not by --library. The
// corpus spans both and an item key is unique only within one, so a single
// scope makes the other library's keys 404 -- which reads as a broken plan
// rather than a misrouted write, and leaves half the backfill silently
// undone.
func runBackfillPlan(ctx context.Context, cmd *cli.Command) error {
	plans, err := backfill.Read(updFromJSON)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		outputScoped(ctx, cmd, backfill.CLIResult{Plan: updFromJSON})
		return nil
	}

	cfg, err := requireConfigCoded()
	if err != nil {
		return err
	}
	if !netutil.Online() {
		return cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — applying a plan requires network access")
	}

	total := &backfill.Result{}
	for _, scope := range []zot.LibraryScope{zot.LibPersonal, zot.LibShared} {
		rows := backfill.ByLibrary(plans)[string(scope)]
		if len(rows) == 0 {
			continue
		}
		ref, refErr := cfg.Resolve(scope)
		if refErr != nil {
			return refErr
		}
		c, clientErr := api.New(cfg, api.WithLibrary(ref))
		if clientErr != nil {
			return clientErr
		}
		res, applyErr := backfill.Apply(ctx, c, c, rows)
		if applyErr != nil {
			return applyErr
		}
		total.Merge(res)
	}

	outputScoped(ctx, cmd, backfill.CLIResult{
		Plan: updFromJSON, Planned: len(plans), Result: total,
	})
	return nil
}
