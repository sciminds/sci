package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

func linkCommand() *cli.Command {
	return &cli.Command{
		Name:  "link",
		Usage: "Read what an item is related to",
		Description: "Zotero's \"related items\" (dc:relation), the link its desktop UI\n" +
			"shows in the Related pane.\n\n" +
			"$ sci zot link list NOTEKEY1   # what is this related to?\n\n" +
			"Read-only: creating or removing a relation is a write, and this\n" +
			"surface never writes to your library.",
		Commands: []*cli.Command{
			linkListCommand(),
		},
	}
}

func linkListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Show what an item is related to",
		Description: "$ sci zot link list NOTEKEY1\n\n" +
			"Reads the local Zotero database. A relation written seconds ago\n" +
			"lives only on the server until Zotero desktop syncs it back, so a\n" +
			"link that is missing here may simply be newer than the mirror.",
		ArgsUsage: "<item-key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one item key")
			}
			key := cmd.Args().First()

			rels, err := readRelations(ctx, key)
			if err != nil {
				return err
			}
			referenced := lo.Uniq(append(
				lo.Flatten(lo.Values(rels.Other)), rels.Related...))
			rels.Titles = linkTitles(ctx, referenced...)

			outputScoped(ctx, cmd, zot.LinkListResult{
				Key:       key,
				Relations: rels,
			})
			return nil
		},
	}
}

// readRelations fetches an item's relations from the local database.
func readRelations(ctx context.Context, key string) (local.ItemRelationSet, error) {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return local.ItemRelationSet{}, err
	}
	defer func() { _ = db.Close() }()
	return db.ItemRelations(key)
}

// linkTitles resolves item keys to display labels, best-effort.
//
// A relation is only meaningful if you can tell what is on the other end,
// and an 8-char key doesn't tell you. Failures are swallowed: a label is
// decoration, and a listing that succeeded must not report an error because
// the local DB couldn't name one side.
func linkTitles(ctx context.Context, keys ...string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	labels, err := db.ItemLabels(keys)
	if err != nil {
		return nil
	}
	return labels
}
