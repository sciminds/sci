package cli

import (
	"context"

	"github.com/sciminds/sci/internal/zot"
	"github.com/urfave/cli/v3"
)

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:        "collection",
		Aliases:     []string{"coll"},
		Usage:       "List the collections in your library",
		Description: "$ sci zot collection list\n$ sci zot collection browse\n\nRead-only: creating collections and moving items between them are\nwrites, and this surface never writes to your library.",
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
		},
	}
}

func tagsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tags",
		Aliases:     []string{"tag"},
		Usage:       "List the tags in your library",
		Description: "$ sci zot tags list\n$ sci zot tags browse\n\nRead-only: attaching, removing and deleting tags are writes, and this\nsurface never writes to your library.",
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
		},
	}
}
