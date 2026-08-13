package cli

import (
	"context"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/client"
	"github.com/urfave/cli/v3"
)

// savedSearchListAll is the destination for `saved-search list --all`.
var savedSearchListAll bool

func savedSearchCommand() *cli.Command {
	return &cli.Command{
		Name:    "saved-search",
		Aliases: []string{"ss"},
		Usage:   "Read the Zotero saved searches in your library (list, show)",
		Description: "$ sci zot saved-search list\n" +
			"$ sci zot saved-search show ABCD1234\n" +
			"$ sci zot saved-search show missing-pdf   # by name\n\n" +
			"Both read LIVE from the Zotero Web API — a saved search has no row in\n" +
			"the local mirror worth reading.\n\n" +
			"Creating and editing one is Zotero desktop's job: the Web API stores a\n" +
			"saved search's definition but cannot evaluate it, so a search written\n" +
			"from here would exist and never run.",
		Commands: []*cli.Command{
			savedSearchListCommand(),
			savedSearchShowCommand(),

			savedSearchCreateStub(),
			savedSearchUpdateStub(),
			savedSearchDeleteStub(),
		},
	}
}

func savedSearchListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List every saved search in the library",
		Description: "$ sci zot saved-search list             # active searches only (matches Zotero desktop sidebar)\n" +
			"$ sci zot saved-search list --all       # include trashed searches",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "include trashed saved searches", Destination: &savedSearchListAll, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			raw, err := c.ListSavedSearches(ctx)
			if err != nil {
				return err
			}
			searches := lo.FilterMap(raw, func(s client.Search, _ int) (zot.SavedSearch, bool) {
				ss := savedSearchFromClient(&s)
				if ss.Deleted && !savedSearchListAll {
					return ss, false
				}
				return ss, true
			})
			outputScoped(ctx, cmd, zot.SavedSearchListResult{Count: len(searches), Searches: searches})
			return nil
		},
	}
}

func savedSearchShowCommand() *cli.Command {
	return &cli.Command{
		Name:        "show",
		Aliases:     []string{"read", "get"},
		Usage:       "Show a saved search's name and conditions",
		Description: "$ sci zot saved-search show ABCD1234\n$ sci zot saved-search show missing-pdf   # by name",
		ArgsUsage:   "<key-or-name>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a saved-search key or name")
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			s, err := c.ResolveSavedSearch(ctx, cmd.Args().First())
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.SavedSearchResult{Search: savedSearchFromClient(s)})
			return nil
		},
	}
}

// savedSearchFromClient converts a generated client.Search to the cmdutil-
// facing zot.SavedSearch shape.
func savedSearchFromClient(s *client.Search) zot.SavedSearch {
	out := zot.SavedSearch{
		Key:     s.Key,
		Version: s.Version,
		Name:    s.Data.Name,
	}
	if s.Data.Deleted != nil {
		out.Deleted = *s.Data.Deleted
	}
	out.Conditions = lo.Map(s.Data.Conditions, func(c client.SearchCondition, _ int) zot.SavedSearchCondition {
		return zot.SavedSearchCondition{Condition: c.Condition, Operator: c.Operator, Value: c.Value}
	})
	return out
}
