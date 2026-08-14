package cli

import (
	"context"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/urfave/cli/v3"
)

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
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max notes to surface (0 = unlimited)", Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Local: true},
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
			offset := cmd.Int("offset")
			page := paginate(notes, offset, cmd.Int("limit"))
			outputScoped(ctx, cmd, zot.RealNotesListResult{
				Count:  len(page),
				Total:  len(notes),
				Offset: offset,
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
			&cli.BoolFlag{Name: "md", Usage: "also emit the body converted to markdown (--json adds a `markdown` field)", Local: true},
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
			if cmd.Bool("md") {
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
