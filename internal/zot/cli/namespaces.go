package cli

import (
	"context"

	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:    "collection",
		Aliases: []string{"coll"},
		Usage:   "Work with collections (list, browse, create, update, delete, add, remove)",
		Description: "$ sci zot collection list\n" +
			"$ sci zot collection browse\n" +
			"$ sci zot collection create \"Reading list\" --parent Projects\n" +
			"$ sci zot collection add ABC12345 \"Reading list\"\n" +
			"$ sci zot collection remove ABC12345 \"Reading list\"\n" +
			"$ sci zot collection update \"Reading list\" --name \"To read\"\n" +
			"$ sci zot collection delete \"To read\" --yes\n\n" +
			"list and browse read the local mirror. create, update, delete, add\n" +
			"and remove write through the Zotero Web API: they need an API key\n" +
			"(sci zot setup --api-key <key>) and one library (--library\n" +
			"personal|shared), and each returns the server's copy of what it\n" +
			"wrote — the local mirror shows the change only after Zotero desktop\n" +
			"syncs.",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every collection in the library with item counts",
				Description: "$ sci zot collection list",
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
					outputScoped(ctx, cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
					return nil
				},
			},
			collBrowseCommand(),
			collectionCreateCommand(),
			collectionUpdateCommand(),
			collectionDeleteCommand(),
			collectionAddCommand(),
			collectionRemoveCommand(),
		},
	}
}

func tagsCommand() *cli.Command {
	return &cli.Command{
		Name:    "tags",
		Aliases: []string{"tag"},
		Usage:   "Work with tags (list, browse, add, remove)",
		Description: "$ sci zot tags list\n" +
			"$ sci zot tags browse\n" +
			"$ sci zot tags add ABC12345 to-read methods\n" +
			"$ sci zot tags remove ABC12345 to-read\n\n" +
			"list and browse read the local mirror. add and remove write through\n" +
			"the Zotero Web API (an API key and one library are needed) and act\n" +
			"on one item's tags at a time; renaming or deleting a tag across the\n" +
			"library is done in Zotero desktop.",
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
			tagsAddCommand(),
			tagsRemoveCommand(),
		},
	}
}
