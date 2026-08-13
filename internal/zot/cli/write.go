package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

// collListRemote is the destination for `collection list --remote`, the
// one flag left in this file that reaches the Zotero Web API.
var collListRemote bool

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
		return nil, cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — this command reads your library live from Zotero").
			WithTry("re-run when online; the local reads (search, item read/list, bib, export) work offline")
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

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:        "collection",
		Aliases:     []string{"coll"},
		Usage:       "List the collections in your library",
		Description: "$ sci zot collection list\n$ sci zot collection browse\n\nCreating collections and moving items between them is a write, and\nwrites live in the zot binary (`zot collection create`).",
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
			collBrowseCommand(),

			collectionCreateStub(),
			collectionDeleteStub(),
			collectionAddStub(),
			collectionRemoveStub(),
		},
	}
}

func tagsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tags",
		Aliases:     []string{"tag"},
		Usage:       "List the tags in your library",
		Description: "$ sci zot tags list\n$ sci zot tags browse\n\nAttaching, removing and deleting tags is a write, and writes live in\nthe zot binary (`zot tags add`).",
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
			tagsBrowseCommand(),

			tagsAddStub(),
			tagsRemoveStub(),
			tagsDeleteStub(),
		},
	}
}
