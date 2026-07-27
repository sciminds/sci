package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/bib"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/link"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/notemd"
	"github.com/urfave/cli/v3"
)

// link-command flag destinations (package-scoped).
var (
	linkListRemote    bool
	linkRmYes         bool
	linkSuggestAply   bool
	linkSuggestYes    bool
	linkSuggestRemote bool
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
			"$ sci zot link rm NOTEKEY1 PAPERKEY1   # remove the relation (both sides)\n" +
			"$ sci zot link suggest NOTEKEY1        # derive the links from the note's own references",
		Commands: []*cli.Command{
			linkAddCommand(),
			linkListCommand(),
			linkRmCommand(),
			linkSuggestCommand(),
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

func linkSuggestCommand() *cli.Command {
	return &cli.Command{
		Name:  "suggest",
		Usage: "Derive a note's links from the items it references",
		Description: "$ sci zot link suggest NOTEKEY1           # dry-run: what would be linked\n" +
			"$ sci zot link suggest NOTEKEY1 --apply   # write the relations\n" +
			"$ sci zot link suggest NOTEKEY1 --apply --yes\n" +
			"$ sci zot link suggest NOTEKEY1 --remote  # existing links live from Zotero\n\n" +
			"Reads the note, resolves every reference in it — zotero:// item\n" +
			"links, @citekeys, DOIs, arXiv ids, [[wikilinks]] — against the\n" +
			"library, and proposes a relation per item. References that already\n" +
			"have one are reported, not rewritten; references that match no item\n" +
			"(or more than one) are listed rather than guessed at.\n\n" +
			"--remote reads the note's CURRENT relations from the Zotero Web API\n" +
			"instead of the local mirror. Pass it after a recent `link add`: the\n" +
			"mirror lags until Zotero desktop syncs, so a stale read re-proposes\n" +
			"links that already exist.\n\n" +
			"For notes YOU wrote. A docling extraction is the paper's own text,\n" +
			"so its references are the paper's bibliography — see `zot content`.",
		ArgsUsage: "<note-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "apply", Usage: "write the proposed relations (default is a dry run)", Destination: &linkSuggestAply, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &linkSuggestYes, Local: true},
			&cli.BoolFlag{Name: "remote", Usage: "read the note's existing relations from the Zotero Web API instead of the local mirror", Destination: &linkSuggestRemote, Local: true},
		},
		Action: linkSuggestAction,
	}
}

func linkSuggestAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cmdutil.UsageErrorf(cmd, "expected exactly one note key")
	}
	noteKey := cmd.Args().First()

	suggestions, err := planLinkSuggestions(ctx, noteKey)
	if err != nil {
		return err
	}

	// An empty plan still renders, so a --json caller always gets a shape
	// rather than having to distinguish "no output" from "nothing to do".
	if !linkSuggestAply || len(suggestions) == 0 {
		outputScoped(ctx, cmd, zot.LinkSuggestResult{Result: link.DryRun(noteKey, suggestions)})
		return nil
	}

	proposed := lo.CountBy(suggestions, func(s link.Suggestion) bool {
		return s.Status == link.StatusProposed
	})
	if proposed == 0 {
		outputScoped(ctx, cmd, zot.LinkSuggestResult{Result: link.DryRun(noteKey, suggestions)})
		return nil
	}

	prompt := fmt.Sprintf("relate %s to %d item(s) via the Zotero Web API?", noteKey, proposed)
	if done, err := cmdutil.ConfirmOrSkip(linkSuggestYes, prompt); done || err != nil {
		return err
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	var res *link.Result
	err = uikit.RunWithProgress("Linking", func(t *uikit.ProgressTracker) error {
		t.SetTotal(proposed)
		var applyErr error
		res, applyErr = link.Apply(ctx, apiClient, noteKey, suggestions, link.ApplyOptions{
			OnProgress: func(_, _ int) { t.Advance("linked", "") },
		})
		return applyErr
	})
	if err != nil {
		return err
	}

	outputScoped(ctx, cmd, zot.LinkSuggestResult{Result: res})
	return nil
}

// planLinkSuggestions does every read the plan needs — the note body, the
// whole library, the note's current relations — and hands them to the pure
// planner. Local-only: nothing here writes.
func planLinkSuggestions(ctx context.Context, noteKey string) ([]link.Suggestion, error) {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	nd, err := db.ReadNote(noteKey)
	if err != nil {
		return nil, err
	}
	if err := refuseDoclingExtraction(nd); err != nil {
		return nil, err
	}

	// HTMLToMarkdown, not local.NoteText: the latter deletes anchors
	// outright, which takes the zotero:// URI with them — it is the
	// indexing path, not a display renderer.
	body, err := notemd.HTMLToMarkdown(nd.Body)
	if err != nil {
		return nil, err
	}

	items, err := db.ListAll(local.ListFilter{})
	if err != nil {
		return nil, err
	}
	matches, unresolved := bib.ResolveRefs(bib.ScanText(body), items)

	// Which relations already exist is the one input the local mirror can be
	// wrong about — a link written minutes ago lives only on the server until
	// Zotero desktop syncs it back, and reading the stale copy makes `suggest`
	// re-propose ten links that are already there.
	existing, err := existingRelations(ctx, db, noteKey)
	if err != nil {
		return nil, err
	}
	return link.PlanSuggest(noteKey, matches, unresolved, existing), nil
}

func existingRelations(ctx context.Context, db local.Reader, noteKey string) (local.ItemRelationSet, error) {
	if linkSuggestRemote {
		return remoteRelations(ctx, noteKey)
	}
	return db.ItemRelations(noteKey)
}

// refuseDoclingExtraction stops `suggest` on a note that is a paper's own
// text rather than one the user wrote.
//
// Scanning an extraction would walk the PAPER's bibliography — hundreds of
// references, mostly not in the library and none of them the user's own
// curation — and propose relations from all of it. The noun split holds
// here: an extraction is the paper, so it belongs to `zot content`.
func refuseDoclingExtraction(nd *local.NoteDetail) error {
	if !slices.Contains(nd.Tags, extract.DoclingTag) {
		return nil
	}
	return cmdutil.Coded(cmdutil.CodeUsage,
		"%s is a docling extraction — the paper's own text, not a note you wrote", nd.Key).
		WithTry("`link suggest` derives links from the references YOU cited; an extraction's references are the paper's bibliography. Read it with `sci zot content read " + nd.Key + "`.")
}

// linkTitles resolves item keys to display labels, best-effort.
//
// A relation is only meaningful if you can tell what is on the other end,
// and an 8-char key doesn't tell you. Failures are swallowed: a label is
// decoration, and a link that succeeded must not report an error because
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
