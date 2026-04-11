package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbtui "github.com/sciminds/cli/internal/tui/dbtui/app"
	"github.com/sciminds/cli/internal/zot/view"
	"github.com/urfave/cli/v3"
)

// viewCommand launches the dbtui viewer backed by a read-only projection
// of the local Zotero library. Six columns, sorted by Date Added descending,
// one row per top-level content item.
func viewCommand() *cli.Command {
	return &cli.Command{
		Name:        "view",
		Usage:       "Browse your library in an interactive table (read-only)",
		Description: "$ zot view",
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB()
			if err != nil {
				return err
			}
			// view.Store takes ownership of db and closes it in Store.Close.
			store := view.New(db, time.Local)
			defer func() { _ = store.Close() }()

			// ColHints: let Title and Author(s) flex into available space;
			// cap Journal/Publication and Extra so they don't dominate; keep
			// Year and Date Added snug at their content width.
			flex := true
			hints := map[string]dbtui.ColHint{
				"Title":               {Flex: &flex},
				"Author(s)":           {Flex: &flex, Max: 40},
				"Journal/Publication": {Max: 30},
				"Extra":               {Max: 30},
			}

			err = dbtui.Run(store, "zot library",
				dbtui.WithReadOnly(),
				dbtui.WithColHints(hints),
				dbtui.WithInitialTab(view.TableName),
			)
			if errors.Is(err, dbtui.ErrInterrupted) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("zot view: %w", err)
			}
			return nil
		},
	}
}
