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
