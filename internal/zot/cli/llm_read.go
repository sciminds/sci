package cli

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

func llmReadCommand() *cli.Command {
	return &cli.Command{
		Name:        "read",
		Usage:       "Full markdown content of notes with attribution headers",
		Description: "$ zot llm read ABC12345 DEF67890",
		ArgsUsage:   "<parent-key...>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least one parent item key")
			}

			_, db, err := openLocalDB(ctx)
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
				if len(notes) == 0 {
					return fmt.Errorf("no docling note found for %s", parentKey)
				}

				parent, err := db.Read(parentKey)
				if err != nil {
					return fmt.Errorf("read parent %s: %w", parentKey, err)
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

			cmdutil.Output(cmd, zot.LLMReadResult{
				Count:   len(entries),
				Entries: entries,
			})
			return nil
		},
	}
}
