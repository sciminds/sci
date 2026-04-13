package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

func llmCatalogCommand() *cli.Command {
	return &cli.Command{
		Name:        "catalog",
		Usage:       "Compact index of every paper with a docling note",
		Description: "$ zot llm catalog",
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			notes, err := db.ListAllDoclingNotes()
			if err != nil {
				return err
			}

			if len(notes) == 0 {
				cmdutil.Output(cmd, zot.LLMCatalogResult{})
				return nil
			}

			// Hydrate parent metadata (DOI, date) for unique parent keys.
			parentKeys := lo.Uniq(lo.Map(notes, func(n local.DoclingNoteSummary, _ int) string {
				return n.ParentKey
			}))
			parents := make(map[string]*local.Item, len(parentKeys))
			for _, pk := range parentKeys {
				item, err := db.Read(pk)
				if err != nil {
					continue // graceful: missing parent just means empty DOI/date
				}
				parents[pk] = item
			}

			entries := lo.Map(notes, func(n local.DoclingNoteSummary, _ int) zot.LLMCatalogEntry {
				entry := zot.LLMCatalogEntry{
					Key:     n.ParentKey,
					Title:   n.ParentTitle,
					NoteKey: n.NoteKey,
					Tags:    n.Tags,
					IsHTML:  isHTMLNote(n.Body),
				}
				if p, ok := parents[n.ParentKey]; ok {
					entry.DOI = p.DOI
					entry.Date = p.Date
				}
				return entry
			})

			cmdutil.Output(cmd, zot.LLMCatalogResult{
				Count:   len(entries),
				Entries: entries,
			})
			return nil
		},
	}
}
