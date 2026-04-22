package cli

// `zot item note` creates, reads, updates, and lists Zotero note items
// (itemType=note). Notes are items in their own right but with a disjoint
// schema (body, optional parentItem, no title/creators/DOI), so they get
// their own subcommand tree instead of overloading flag sets on `item add`.
//
// Phase 1 (this file): `note add` only. Read / update / list land next.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/notemd"
	"github.com/urfave/cli/v3"
)

// itemNoteStdin is the stdin source for `zot item note add --body -`.
// Overridable by tests.
var itemNoteStdin io.Reader = os.Stdin

// Flag destinations for `item note add`.
var (
	noteBody       string
	noteBodyFile   string
	noteCollection string
	noteTag        []string
	noteHTML       bool
)

// Flag destinations for `item note update`.
var (
	noteUpdBody     string
	noteUpdBodyFile string
	noteUpdHTML     bool
)

// Flag destinations for `item note read`.
var (
	noteReadHTML bool
)

// itemNoteCommand is the `zot item note` subcommand tree. Registered from
// cli.go under `item`.
func itemNoteCommand() *cli.Command {
	return &cli.Command{
		Name:  "note",
		Usage: "Create, read, update, and list Zotero note items",
		Description: "$ zot item note add --collection COLL1234 --body-file summary.md\n" +
			"$ zot item note add PARENT12 --body \"Quick thought on this paper\"\n" +
			"$ zot item note add --collection COLL1234 --body - < summary.md\n" +
			"\n" +
			"Notes are stored as HTML. By default --body / --body-file is parsed\n" +
			"as markdown and rendered; pass --html to write literal HTML.",
		Commands: []*cli.Command{
			itemNoteAddCommand(),
			itemNoteReadCommand(),
			itemNoteUpdateCommand(),
			itemNoteListCommand(),
		},
	}
}

func itemNoteAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "Create a note item (standalone or attached to a parent)",
		ArgsUsage: "[parent-key]",
		Description: "$ zot item note add --collection COLL1234 --body-file summary.md\n" +
			"$ zot item note add PARENT12 --body \"Quick thought\" --tag idea\n" +
			"$ zot item note add --collection COLL1234 --body - < summary.md\n" +
			"\n" +
			"Pass a parent item key positionally to attach the note as a child.\n" +
			"Omit the positional and pass --collection to create a standalone note.\n" +
			"Both are accepted simultaneously. One body source is required:\n" +
			"--body (inline), --body-file (path), or either set to `-` for stdin.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "body", Usage: "inline note body (or `-` to read from stdin)", Destination: &noteBody, Local: true},
			&cli.StringFlag{Name: "body-file", Usage: "path to a note body file (or `-` for stdin)", Destination: &noteBodyFile, Local: true},
			&cli.StringFlag{Name: "collection", Usage: "place note in this collection (key)", Destination: &noteCollection, Local: true},
			&cli.StringSliceFlag{Name: "tag", Usage: "attach a tag (repeatable)", Destination: &noteTag, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "treat --body/--body-file as raw HTML (still sanitized); default is markdown", Destination: &noteHTML, Local: true},
		},
		Action: runItemNoteAdd,
	}
}

func runItemNoteAdd(ctx context.Context, cmd *cli.Command) error {
	parent := ""
	if cmd.Args().Len() > 0 {
		parent = cmd.Args().First()
	}
	if err := validateNoteTarget(parent, noteCollection); err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	src, err := readNoteBody(noteBody, noteBodyFile, itemNoteStdin)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	html, err := renderNoteBody(src, noteHTML)
	if err != nil {
		return fmt.Errorf("render note body: %w", err)
	}
	data := buildNoteItemData(html, parent, noteCollection, noteTag)

	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	it, err := c.CreateItem(ctx, data)
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, zot.WriteResult{
		Action: "created",
		Kind:   "item",
		Target: it.Key,
		Data:   api.ItemFromClient(it),
	})
	return nil
}

// readNoteBody resolves the note body from exactly one of:
//   - bodyFlag: inline string from --body (or `-` for stdin)
//   - bodyFile: path from --body-file (or `-` for stdin)
//
// Returns an error if both or neither source is provided.
func readNoteBody(bodyFlag, bodyFile string, stdin io.Reader) (string, error) {
	switch {
	case bodyFlag != "" && bodyFile != "":
		return "", errors.New("--body and --body-file are mutually exclusive")
	case bodyFlag == "" && bodyFile == "":
		return "", errors.New("--body or --body-file is required")
	case bodyFlag == "-" || bodyFile == "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	case bodyFlag != "":
		return bodyFlag, nil
	default:
		b, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", fmt.Errorf("read --body-file: %w", err)
		}
		return string(b), nil
	}
}

// renderNoteBody converts raw input into sanitized HTML suitable for the
// Zotero `note` field. With rawHTML=false (default) src is treated as
// markdown and rendered via goldmark; with rawHTML=true src is treated as
// literal HTML. Both paths pass through the same sanitizer.
func renderNoteBody(src string, rawHTML bool) (string, error) {
	if rawHTML {
		return notemd.SanitizeHTML(src), nil
	}
	return notemd.MarkdownToHTML([]byte(src))
}

// buildNoteItemData composes an ItemData payload for `zot item note add`.
// Both parent and collection are optional at this layer — validation
// (require at least one) is done upstream by validateNoteTarget so the
// helper stays composable.
func buildNoteItemData(htmlBody, parent, collection string, tags []string) client.ItemData {
	body := htmlBody
	data := client.ItemData{
		ItemType: client.Note,
		Note:     &body,
	}
	if parent != "" {
		p := parent
		data.ParentItem = &p
	}
	if collection != "" {
		colls := []string{collection}
		data.Collections = &colls
	}
	if len(tags) > 0 {
		ts := lo.Map(tags, func(t string, _ int) client.Tag { return client.Tag{Tag: t} })
		data.Tags = &ts
	}
	return data
}

// validateNoteTarget enforces that a note is anchored somewhere — either
// attached to a parent item or placed in a collection. Both may be set.
func validateNoteTarget(parent, collection string) error {
	if parent == "" && collection == "" {
		return errors.New("a note must have a parent item (positional arg) or a --collection")
	}
	return nil
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
		Description: "$ zot item note read NOTE1234\n" +
			"$ zot item note read NOTE1234 --html       # raw HTML\n" +
			"$ zot item note read NOTE1234 --json       # structured, incl. HTML body",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "html", Usage: "show raw HTML instead of stripping tags (human mode only)", Destination: &noteReadHTML, Local: true},
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
	cmdutil.Output(cmd, noteReadResultFromItem(it, noteReadHTML))
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
	return fmt.Errorf("item is a %s, not a note — use `zot item read` for bibliographic items", itemType)
}

// itemNoteUpdateCommand: `zot item note update KEY` — PATCH a note's body.
// Body input matches `item note add` (--body / --body-file / stdin via `-`);
// --html opts out of markdown rendering. Only the body is touched — tags,
// parent, and collections are untouched.
func itemNoteUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:      "update",
		Usage:     "Replace a note's body in place",
		ArgsUsage: "<key>",
		Description: "$ zot item note update NOTE1234 --body-file revised.md\n" +
			"$ zot item note update NOTE1234 --body \"Updated thought\"\n" +
			"$ zot item note update NOTE1234 --body - < revised.md",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "body", Usage: "inline note body (or `-` to read from stdin)", Destination: &noteUpdBody, Local: true},
			&cli.StringFlag{Name: "body-file", Usage: "path to a note body file (or `-` for stdin)", Destination: &noteUpdBodyFile, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "treat --body/--body-file as raw HTML (still sanitized); default is markdown", Destination: &noteUpdHTML, Local: true},
		},
		Action: runItemNoteUpdate,
	}
}

func runItemNoteUpdate(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() == 0 {
		return cmdutil.UsageErrorf(cmd, "expected a note key")
	}
	key := cmd.Args().First()
	src, err := readNoteBody(noteUpdBody, noteUpdBodyFile, itemNoteStdin)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	html, err := renderNoteBody(src, noteUpdHTML)
	if err != nil {
		return fmt.Errorf("render note body: %w", err)
	}

	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	if err := c.UpdateItem(ctx, key, buildNoteUpdatePatch(html)); err != nil {
		return err
	}
	cmdutil.Output(cmd, zot.WriteResult{
		Action: "updated",
		Kind:   "item",
		Target: key,
	})
	return nil
}

// buildNoteUpdatePatch builds a minimal PATCH body for a note update.
// Only ItemType (required by the shared version-retry path) and Note are
// set; tags / parent / collections are left untouched on the server side.
func buildNoteUpdatePatch(htmlBody string) client.ItemData {
	body := htmlBody
	return client.ItemData{
		ItemType: client.Note,
		Note:     &body,
	}
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
		Description: "$ zot item note list PAPER567\n" +
			"\n" +
			"For notes in a collection use `zot item list --type note --collection COLL`.",
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
	cmdutil.Output(cmd, zot.NoteItemListResult{
		ParentKey: parent,
		Count:     len(entries),
		Notes:     entries,
	})
	return nil
}
