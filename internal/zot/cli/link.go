package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

// link-command flag destinations (package-scoped).
var linkListRemote bool

func linkCommand() *cli.Command {
	return &cli.Command{
		Name:  "link",
		Usage: "Read what an item is related to",
		Description: "Zotero's \"related items\" (dc:relation), the link its desktop UI\n" +
			"shows in the Related pane.\n\n" +
			"$ sci zot link list NOTEKEY1           # what is this related to?\n" +
			"$ sci zot link list NOTEKEY1 --remote  # live from Zotero\n\n" +
			"Reading is what sci keeps. Writing a relation — `link add`,\n" +
			"`link rm`, and the apply half of `link suggest` — is a credentialed\n" +
			"write, so it lives in the zot binary; those names stay registered\n" +
			"here and say where they went.",
		Commands: []*cli.Command{
			linkAddStub(),
			linkListCommand(),
			linkRmStub(),
			linkSuggestStub(),
		},
	}
}

func linkListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "Show what an item is related to",
		Description: "$ sci zot link list NOTEKEY1\n" +
			"$ sci zot link list NOTEKEY1 --remote   # live from Zotero\n\n" +
			"Reads the local mirror by default. A relation written seconds\n" +
			"ago lives only on the server until Zotero desktop syncs it back,\n" +
			"so pass --remote to see one `zot link add` just made.",
		ArgsUsage: "<item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remote", Usage: "read from the Zotero Web API instead of the local mirror", Destination: &linkListRemote, Local: true},
		},
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
				Remote:    linkListRemote,
			})
			return nil
		},
	}
}

// readRelations fetches an item's relations from whichever source the
// --remote flag selected, normalizing the API's predicate map into the same
// shape the local reader returns.
func readRelations(ctx context.Context, key string) (local.ItemRelationSet, error) {
	if !linkListRemote {
		_, db, err := openLocalDB(ctx)
		if err != nil {
			return local.ItemRelationSet{}, err
		}
		defer func() { _ = db.Close() }()
		return db.ItemRelations(key)
	}
	return remoteRelations(ctx, key)
}

// remoteRelations reads an item's relations from the Zotero Web API,
// normalizing the predicate map into the local reader's shape. This is
// ground truth — the local mirror only catches up when Zotero desktop syncs.
func remoteRelations(ctx context.Context, key string) (local.ItemRelationSet, error) {
	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return local.ItemRelationSet{}, err
	}
	byPredicate, err := apiClient.ItemRelations(ctx, key)
	if err != nil {
		return local.ItemRelationSet{}, err
	}
	out := local.ItemRelationSet{Related: byPredicate[local.RelatedPredicate]}
	for pred, keys := range byPredicate {
		if pred == local.RelatedPredicate {
			continue
		}
		if out.Other == nil {
			out.Other = map[string][]string{}
		}
		out.Other[pred] = keys
	}
	return out, nil
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
