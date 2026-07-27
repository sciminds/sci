package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// link-command flag destinations (package-scoped).
var (
	linkListRemote bool
	linkRmYes      bool
)

func linkCommand() *cli.Command {
	return &cli.Command{
		Name:  "link",
		Usage: "Relate two items — a note and the paper it discusses",
		Description: "Zotero's \"related items\" (dc:relation), the link its desktop UI\n" +
			"shows in the Related pane. Relations are bidirectional: linking\n" +
			"writes both sides, so the pair shows up on each item.\n\n" +
			"$ sci zot link add NOTEKEY1 PAPERKEY1  # relate a note to the paper it discusses\n" +
			"$ sci zot link list NOTEKEY1           # what is this related to?\n" +
			"$ sci zot link rm NOTEKEY1 PAPERKEY1   # remove the relation (both sides)",
		Commands: []*cli.Command{
			linkAddCommand(),
			linkListCommand(),
			linkRmCommand(),
		},
	}
}

// The pair-relating verb is `add` rather than a bare `zot link A B`
// because a namespace parent can't also take positionals: every parent
// goes through cmdutil.WireNamespaceDefaults, which rejects an unknown
// child, so the first key would be read as a misspelled subcommand.
// `link add` also matches the add/rm shape of `item note add` and
// `collection add`.
func linkAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Relate two items",
		Description: "$ sci zot link add NOTEKEY1 PAPERKEY1\n\n" +
			"Writes both directions, so the relation shows on each item in\n" +
			"Zotero. Re-running is a no-op per side, so a retry after a\n" +
			"partial failure repairs the missing half.",
		ArgsUsage: "<key-a> <key-b>",
		Action:    linkAddAction,
	}
}

func linkAddAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 2 {
		return cmdutil.UsageErrorf(cmd, "expected exactly two item keys")
	}
	a, b := cmd.Args().Get(0), cmd.Args().Get(1)

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	if err := apiClient.LinkItems(ctx, a, b); err != nil {
		return err
	}

	outputScoped(ctx, cmd, zot.LinkResult{A: a, B: b, Titles: linkTitles(ctx, a, b)})
	return nil
}

func linkRmCommand() *cli.Command {
	return &cli.Command{
		Name:    "rm",
		Aliases: []string{"remove", "unlink"},
		Usage:   "Remove the relation between two items (both sides)",
		Description: "$ sci zot link rm NOTEKEY1 PAPERKEY1\n" +
			"$ sci zot link rm NOTEKEY1 PAPERKEY1 --yes  # skip confirmation\n\n" +
			"Only dc:relation is removable. Zotero's own owl:sameAs and\n" +
			"dc:replaces are left alone — it maintains those itself.",
		ArgsUsage: "<key-a> <key-b>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &linkRmYes, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 2 {
				return cmdutil.UsageErrorf(cmd, "expected exactly two item keys")
			}
			a, b := cmd.Args().Get(0), cmd.Args().Get(1)

			if done, err := cmdutil.ConfirmOrSkip(linkRmYes,
				"Remove the relation between "+a+" and "+b+"?"); done || err != nil {
				return err
			}

			apiClient, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			if err := apiClient.UnlinkItems(ctx, a, b); err != nil {
				return err
			}

			outputScoped(ctx, cmd, zot.LinkResult{
				A: a, B: b, Removed: true, Titles: linkTitles(ctx, a, b),
			})
			return nil
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
			"so pass --remote to see one you just made.",
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

			outputScoped(ctx, cmd, zot.LinkListResult{
				Key:       key,
				Relations: rels,
				Titles:    linkTitles(ctx, referenced...),
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

// linkTitles resolves item keys to titles for display, best-effort.
//
// A relation is only meaningful if you can tell what is on the other end,
// and an 8-char key doesn't tell you. Failures are swallowed: a title is
// decoration, and a link that succeeded must not report an error because
// the local DB couldn't name one side. Notes have no title field, so they
// fall back to their body snippet.
func linkTitles(ctx context.Context, keys ...string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	out := map[string]string{}
	for _, k := range keys {
		if it, err := db.Read(k); err == nil && it.Title != "" {
			out[k] = it.Title
			continue
		}
		if nd, err := db.ReadNote(k); err == nil {
			out[k] = zot.NoteLabel(nd.Title, nd.Body)
		}
	}
	return out
}
