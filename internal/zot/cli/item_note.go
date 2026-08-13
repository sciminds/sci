package cli

// `zot item note` reads Zotero note items (itemType=note) LIVE from the
// Web API. Notes are items in their own right but with a disjoint schema
// (body, optional parentItem, no title/creators/DOI), so they get their
// own subcommand tree rather than overloading `item read`.
//
// Writing a note is a credentialed write and lives in the zot binary;
// `add` and `update` stay here only as stubs that say so.

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/urfave/cli/v3"
)

// Flag destinations for `item note read`.
var (
	noteReadHTML bool
	noteReadMD   bool
)

// itemNoteCommand is the `zot item note` subcommand tree. Registered from
// cli.go under `item`.
func itemNoteCommand() *cli.Command {
	return &cli.Command{
		Name:  "note",
		Usage: "Read Zotero note items live from the Web API",
		Description: "$ sci zot item note read NOTE1234\n" +
			"$ sci zot item note read NOTE1234 --md --json\n" +
			"$ sci zot item note list PAPER567\n" +
			"\n" +
			"Both read LIVE from the Zotero Web API, so they see a note written\n" +
			"seconds ago that the local mirror has not synced yet. Writing one\n" +
			"is `zot item note add` in the zot binary.",
		Commands: []*cli.Command{
			itemNoteReadCommand(),
			itemNoteListCommand(),

			itemNoteAddStub(),
			itemNoteUpdateStub(),
		},
	}
}

// itemNoteReadCommand: `zot item note read KEY` — fetch a note item and
// print its body. Human mode strips HTML tags for terminal readability;
// --html preserves raw HTML; --json always returns structured data with
// the HTML body intact.
func itemNoteReadCommand() *cli.Command {
	return &cli.Command{
		Name:      "read",
		Usage:     "Show a note item's body, parent, tags, and collections",
		ArgsUsage: "<key>",
		Description: "$ sci zot item note read NOTE1234\n" +
			"$ sci zot item note read NOTE1234 --html       # raw stored body\n" +
			"$ sci zot item note read NOTE1234 --md --json  # structured, plus a markdown field",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "html", Usage: "show the raw stored body instead of rendering markdown (human mode only)", Destination: &noteReadHTML, Local: true},
			&cli.BoolFlag{Name: "md", Usage: "also emit the body converted to markdown (--json adds a `markdown` field)", Destination: &noteReadMD, Local: true},
		},
		Action: runItemNoteRead,
	}
}

func runItemNoteRead(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return cmdutil.UsageErrorf(cmd, "expected a note key")
	}
	key := cmd.Args().First()
	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	it, err := c.GetItem(ctx, key)
	if err != nil {
		return err
	}
	if err := assertNoteType(string(it.Data.ItemType)); err != nil {
		return err
	}
	res := noteReadResultFromItem(it, noteReadHTML)
	if noteReadMD {
		md, err := notemd.HTMLToMarkdown(res.Body)
		if err != nil {
			return err
		}
		res.Markdown = md
	}
	outputScoped(ctx, cmd, res)
	return nil
}

// noteReadResultFromItem projects a client.Item into the CLI result shape.
// Kept as a thin pure helper so the hydration is easy to eyeball in tests
// (though the CLI Action itself is covered via live smoke, not mocked).
func noteReadResultFromItem(it *client.Item, showHTML bool) zot.NoteItemReadResult {
	out := zot.NoteItemReadResult{
		Key:      it.Key,
		ShowHTML: showHTML,
	}
	if it.Data.Note != nil {
		out.Body = *it.Data.Note
	}
	if it.Data.ParentItem != nil {
		out.ParentItem = *it.Data.ParentItem
	}
	if it.Data.Collections != nil {
		out.Collections = append(out.Collections, *it.Data.Collections...)
	}
	if it.Data.Tags != nil {
		out.Tags = lo.Map(*it.Data.Tags, func(t client.Tag, _ int) string { return t.Tag })
	}
	if it.Data.DateAdded != nil {
		out.DateAdded = it.Data.DateAdded.Format(dateLayout)
	}
	if it.Data.DateModified != nil {
		out.DateModified = it.Data.DateModified.Format(dateLayout)
	}
	return out
}

// dateLayout matches Zotero's API timestamp format (RFC 3339 with second
// precision, Z-suffixed UTC) — same shape the Web API emits on reads.
const dateLayout = "2006-01-02T15:04:05Z07:00"

// assertNoteType rejects keys whose item type isn't `note`. Nudges the
// user toward `zot item read` for bibliographic reads.
func assertNoteType(itemType string) error {
	if itemType == string(client.Note) {
		return nil
	}
	if itemType == "" {
		return errors.New("item has no type — cannot confirm it's a note")
	}
	return fmt.Errorf("item is a %s, not a note — use `sci zot item read` for bibliographic items", itemType)
}

// itemNoteListCommand: `zot item note list PARENT` — list note children of
// a parent item. Filtering by collection / tag goes through `zot item list`
// (though that command's --type filter is currently broken; see known-bugs
// doc). Minimal scope here: the one case `item list` cannot cover cleanly.
func itemNoteListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List note children of a parent item",
		ArgsUsage: "<parent-key>",
		Description: "$ sci zot item note list PAPER567\n" +
			"\n" +
			"For notes in a collection use `sci zot item list --type note --collection COLL`.",
		Action: runItemNoteList,
	}
}

func runItemNoteList(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return cmdutil.UsageErrorf(cmd, "expected a parent item key")
	}
	parent := cmd.Args().First()
	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	children, err := c.ListNoteChildren(ctx, parent)
	if err != nil {
		return err
	}
	entries := lo.Map(children, func(n api.NoteChild, _ int) zot.NoteItemListEntry {
		return zot.NoteItemListEntry{Key: n.Key, Body: n.Body, Tags: n.Tags}
	})
	outputScoped(ctx, cmd, zot.NoteItemListResult{
		ParentKey: parent,
		Count:     len(entries),
		Notes:     entries,
	})
	return nil
}
