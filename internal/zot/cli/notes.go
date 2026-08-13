package cli

import (
	"context"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/urfave/cli/v3"
)

// notes-command flag destinations (package-scoped).
var (
	notesReadMD bool

	notesListLimit  int
	notesListOffset int
)

// extractionMoved is the shared explanation on every retired extraction
// verb under `notes`: one sentence on what changed and why, so the error
// teaches the model rather than just redirecting.
//
// Two moves are folded into it, because a user typing `zot notes add`
// today is two renames behind. The verbs first left `notes` for `content`
// (an extraction is the paper's text, not a note — 4,710 of 4,752 "notes"
// in the live library were extractions), and then extraction itself left
// sci, because running docling and posting the result back is a
// credentialed write.
const extractionMoved = "an extraction is the paper's text, not a note, and producing one " +
	"is a credentialed write — extraction lives in the zot binary now, and `zot notes` " +
	"means the notes you wrote"

func notesCommand() *cli.Command {
	return &cli.Command{
		Name:    "notes",
		Aliases: []string{"note"},
		Usage:   "The notes YOU wrote (list, read)",
		Description: "Notes you authored — not docling extractions. Extractions are the\n" +
			"paper's text, and both making them and reading them live in the zot\n" +
			"binary (`zot extract-lib`, `zot read`).\n\n" +
			"$ sci zot notes list              # your notes, attached and standalone\n" +
			"$ sci zot notes read NOTECH11     # one note's body\n" +
			"$ sci zot notes read NOTECH11 --md --json   # markdown, for piping",
		Commands: []*cli.Command{
			notesListCommand(),
			notesReadCommand(),

			// The moved verbs stay registered so they can explain
			// themselves; urfave would otherwise answer with a bare
			// "command not found".
			movedToZotCommand("add", "moved to `zot extract-lib`",
				[]string{"notes", "add"}, "zot extract-lib", extractionMoved),
			movedToZotCommand("update", "moved to `zot extract-lib --reextract`",
				[]string{"notes", "update"}, "zot extract-lib --reextract", extractionMoved),
			movedToZotCommand("delete", "moved to `zot extract-lib`",
				[]string{"notes", "delete"}, "zot extract-lib", extractionMoved),
		},
	}
}

func notesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List the notes you wrote",
		Description: "$ sci zot notes list                 # your notes (attached + standalone)\n" +
			"$ sci zot notes list --limit 0       # all of them\n" +
			"$ sci zot notes list --limit 25 --offset 50   # paginate\n\n" +
			"Docling extractions are excluded — they are the paper's text, not\n" +
			"a note. Read those with `zot read` in the zot binary.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max notes to surface (0 = unlimited)", Destination: &notesListLimit, Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Destination: &notesListOffset, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			notes, err := db.ListNotes()
			if err != nil {
				return err
			}
			page := paginate(notes, notesListOffset, notesListLimit)
			outputScoped(ctx, cmd, zot.RealNotesListResult{
				Count:  len(page),
				Total:  len(notes),
				Offset: notesListOffset,
				Notes:  page,
			})
			return nil
		},
	}
}

// paginate slices xs[offset:offset+limit] safely. limit<=0 means
// "everything from offset". An out-of-range offset returns an empty
// slice rather than an error — the caller's footer/Total still tells
// the user how many they could have seen.
func paginate[T any](xs []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(xs) {
		return nil
	}
	end := len(xs)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return xs[offset:end]
}

func notesReadCommand() *cli.Command {
	return &cli.Command{
		Name:  "read",
		Usage: "Show the full body of a note",
		Description: "$ sci zot notes read NOTECH10\n" +
			"$ sci zot notes read NOTECH10 --md --json   # body as markdown, for piping into a model",
		ArgsUsage: "<note-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "md", Usage: "also emit the body converted to markdown (--json adds a `markdown` field)", Destination: &notesReadMD, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a note key")
			}
			noteKey := cmd.Args().First()
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			nd, err := db.ReadNote(noteKey)
			if err != nil {
				return err
			}
			res := zot.NoteReadResult{Note: *nd}
			if notesReadMD {
				md, err := notemd.HTMLToMarkdown(nd.Body)
				if err != nil {
					return err
				}
				res.Markdown = md
			}
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}
