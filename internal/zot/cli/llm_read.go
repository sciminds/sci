package cli

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

func llmReadCommand() *cli.Command {
	return &cli.Command{
		Name:  "read",
		Usage: "Full markdown content of notes with attribution headers",
		Description: "$ sci zot llm read ABC12345 DEF67890\n\n" +
			"A paper whose markdown was too large for Zotero to store has no\n" +
			"note; its text is served from the layout dir instead, marked\n" +
			"source=layout.",
		ArgsUsage: "<parent-key...>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least one parent item key")
			}

			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			keys := cmd.Args().Slice()
			entries := make([]zot.LLMReadEntry, 0, len(keys))
			for _, parentKey := range keys {
				notes, err := db.ListDoclingNotes(parentKey)
				if err != nil {
					return fmt.Errorf("list notes for %s: %w", parentKey, err)
				}
				// The note is the primary store; the layout dir answers
				// only when there is no note at all. Resolved before the
				// parent read so a key in neither store keeps its original
				// error.
				var layoutBody string
				if len(notes) == 0 {
					body, ok, err := layoutExtraction(cfg, db, parentKey)
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("no docling note found for %s", parentKey)
					}
					layoutBody = body
				}

				parent, err := db.Read(parentKey)
				if err != nil {
					return fmt.Errorf("read parent %s: %w", parentKey, err)
				}

				if len(notes) == 0 {
					entries = append(entries, zot.LLMReadEntry{
						Key:    parentKey,
						Title:  parent.Title,
						DOI:    parent.DOI,
						Source: SourceLayout,
						Body:   layoutBody,
					})
					continue
				}

				noteEntries := lo.Map(notes, func(ch local.ChildItem, _ int) zot.LLMReadEntry {
					return zot.LLMReadEntry{
						Key:     parentKey,
						Title:   parent.Title,
						DOI:     parent.DOI,
						NoteKey: ch.Key,
						Body:    noteBodyForMQ(ch.Note),
					}
				})
				entries = append(entries, noteEntries...)
			}

			outputScoped(ctx, cmd, zot.LLMReadResult{
				Count:   len(entries),
				Entries: entries,
			})
			return nil
		},
	}
}
