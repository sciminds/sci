package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/urfave/cli/v3"
)

// Saved-search command flag destinations (package-scoped).
//
// NOTE: --condition is intentionally NOT marked Local: true. urfave/cli v3
// re-runs PreParse on every Set call for Local flags, and SliceBase.Create
// zeroes the slice each time — so a Local slice flag keeps only the LAST
// occurrence. The fix is to drop Local (with a lint waiver). The flag is
// on a leaf command, so leakage to subcommands is not a concern. See
// internal/zot/CLAUDE.md "slice-flag Local quirk" for the full story.
var (
	savedSearchListAll        bool
	savedSearchCreateJoinAny  bool
	savedSearchCreateFromJSON string
	savedSearchUpdateName     string
	savedSearchUpdateJoinAny  bool
	savedSearchUpdateFromJSON string
	savedSearchDeleteYes      bool
)

// savedSearchStdin is overridable by tests for `--from-json -`.
var savedSearchStdin io.Reader = os.Stdin

func savedSearchCommand() *cli.Command {
	return &cli.Command{
		Name:    "saved-search",
		Aliases: []string{"ss"},
		Usage:   "Manage Zotero saved searches (list, show, create, update, delete)",
		Description: "$ sci zot saved-search list\n" +
			"$ sci zot saved-search show ABCD1234\n" +
			"$ sci zot saved-search create \"Recent ML\" --condition title:contains:transformer --condition dateAdded:isInTheLast:30 days\n" +
			"$ sci zot saved-search create \"Either\" --any --condition tag:is:ml --condition tag:is:nlp\n" +
			"$ sci zot saved-search create \"From file\" --from-json conds.json\n" +
			"$ sci zot saved-search update ABCD1234 --name \"Renamed\" --condition title:contains:gpt\n" +
			"$ sci zot saved-search delete ABCD1234",
		Commands: []*cli.Command{
			savedSearchListCommand(),
			savedSearchShowCommand(),
			savedSearchCreateCommand(),
			savedSearchUpdateCommand(),
			savedSearchDeleteCommand(),
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
		Description: "$ sci zot saved-search show ABCD1234",
		ArgsUsage:   "<key>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a saved-search key")
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			s, err := c.GetSavedSearch(ctx, cmd.Args().First())
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.SavedSearchResult{Search: savedSearchFromClient(s)})
			return nil
		},
	}
}

func savedSearchCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new saved search",
		Description: "$ sci zot saved-search create \"Recent ML\" --condition title:contains:transformer --condition dateAdded:isInTheLast:30 days\n" +
			"$ sci zot saved-search create \"Either\" --any --condition tag:is:ml --condition tag:is:nlp\n" +
			"$ sci zot saved-search create \"From JSON\" --from-json conds.json   # array of {condition,operator,value}\n" +
			"$ cat conds.json | zot saved-search create \"Piped\" --from-json -\n" +
			"\n" +
			"Conditions use the form 'condition:operator:value'. The value may itself contain colons —\n" +
			"only the first two are split. Common conditions: title, creator, tag, itemType, dateAdded,\n" +
			"dateModified, fulltextContent, collection. Operators: is, isNot, contains, doesNotContain,\n" +
			"beginsWith, before, after, isInTheLast.",
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "condition", Aliases: []string{"c"}, Usage: "condition triple 'field:operator:value' (repeatable)"}, // lint:no-local — urfave/cli v3 SliceFlag + Local:true keeps only the last --condition (PreParse re-creates the slice on every Set); see CLAUDE.md "slice-flag Local quirk"
			&cli.BoolFlag{Name: "any", Usage: "match ANY condition instead of ALL (adds a leading joinMode condition)", Destination: &savedSearchCreateJoinAny, Local: true},
			&cli.StringFlag{Name: "from-json", Usage: "read conditions as a JSON array from file or '-' for stdin", Destination: &savedSearchCreateFromJSON, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a saved-search name")
			}
			name := cmd.Args().First()
			conds, err := buildSavedSearchConditions(cmd.StringSlice("condition"), savedSearchCreateFromJSON, savedSearchCreateJoinAny, savedSearchStdin)
			if err != nil {
				return cmdutil.UsageErrorf(cmd, "%v", err)
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			s, err := c.CreateSavedSearch(ctx, name, conds)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "created",
				Kind:    "saved-search",
				Target:  s.Key,
				Message: fmt.Sprintf("created saved search %q (%s)", name, s.Key),
				Data:    savedSearchFromClient(s),
			})
			return nil
		},
	}
}

func savedSearchUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Replace a saved search's name and/or conditions",
		Description: "$ sci zot saved-search update ABCD1234 --name \"Renamed\"\n" +
			"$ sci zot saved-search update ABCD1234 --condition title:contains:gpt --condition itemType:is:journalArticle\n" +
			"\n" +
			"Saved-search updates are full replacements (the Zotero API has no per-condition PATCH).\n" +
			"Omit --name to keep the existing name; omit --condition / --from-json to keep existing\n" +
			"conditions.",
		ArgsUsage: "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "new display name", Destination: &savedSearchUpdateName, Local: true},
			&cli.StringSliceFlag{Name: "condition", Aliases: []string{"c"}, Usage: "condition triple 'field:operator:value' (repeatable; replaces all existing conditions)"}, // lint:no-local — urfave/cli v3 SliceFlag + Local:true keeps only the last --condition (PreParse re-creates the slice on every Set); see CLAUDE.md "slice-flag Local quirk"
			&cli.BoolFlag{Name: "any", Usage: "match ANY condition instead of ALL (adds a leading joinMode condition)", Destination: &savedSearchUpdateJoinAny, Local: true},
			&cli.StringFlag{Name: "from-json", Usage: "read conditions as a JSON array from file or '-' for stdin", Destination: &savedSearchUpdateFromJSON, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a saved-search key")
			}
			key := cmd.Args().First()
			condFlags := cmd.StringSlice("condition")
			if savedSearchUpdateName == "" && len(condFlags) == 0 && savedSearchUpdateFromJSON == "" {
				return cmdutil.UsageErrorf(cmd, "at least one of --name, --condition, --from-json is required")
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			cur, err := c.GetSavedSearch(ctx, key)
			if err != nil {
				return err
			}
			name := savedSearchUpdateName
			if name == "" {
				name = cur.Data.Name
			}
			var conds []client.SearchCondition
			if len(condFlags) > 0 || savedSearchUpdateFromJSON != "" {
				conds, err = buildSavedSearchConditions(condFlags, savedSearchUpdateFromJSON, savedSearchUpdateJoinAny, savedSearchStdin)
				if err != nil {
					return cmdutil.UsageErrorf(cmd, "%v", err)
				}
			} else {
				conds = cur.Data.Conditions
			}
			if err := c.UpdateSavedSearch(ctx, key, name, conds); err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "updated",
				Kind:    "saved-search",
				Target:  key,
				Message: fmt.Sprintf("updated saved search %q (%s)", name, key),
			})
			return nil
		},
	}
}

func savedSearchDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Aliases:     []string{"trash"},
		Usage:       "Delete a saved search (items are untouched)",
		Description: "$ sci zot saved-search delete ABCD1234\n$ sci zot saved-search delete ABCD1234 --yes",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &savedSearchDeleteYes, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a saved-search key")
			}
			key := cmd.Args().First()
			if done, err := cmdutil.ConfirmOrSkip(savedSearchDeleteYes, fmt.Sprintf("Delete saved search %s?", key)); done || err != nil {
				return err
			}
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			if err := c.DeleteSavedSearch(ctx, key); err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.WriteResult{Action: "deleted", Kind: "saved-search", Target: key})
			return nil
		},
	}
}

// buildSavedSearchConditions assembles the wire-shaped condition slice from
// CLI inputs. Either --condition (repeatable) or --from-json may be used —
// but not both, since the JSON form is meant as a "give me everything" path
// and mixing the two would be ambiguous about ordering. When --any is set,
// a leading joinMode pseudo-condition is prepended.
func buildSavedSearchConditions(condFlags []string, fromJSON string, joinAny bool, stdin io.Reader) ([]client.SearchCondition, error) {
	if len(condFlags) > 0 && fromJSON != "" {
		return nil, fmt.Errorf("--condition and --from-json are mutually exclusive")
	}
	if len(condFlags) == 0 && fromJSON == "" {
		return nil, fmt.Errorf("provide at least one --condition or --from-json")
	}

	var conds []client.SearchCondition
	if fromJSON != "" {
		raw, err := readSavedSearchJSON(fromJSON, stdin)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &conds); err != nil {
			return nil, fmt.Errorf("parse --from-json: %w", err)
		}
		for i, c := range conds {
			if c.Condition == "" || c.Operator == "" {
				return nil, fmt.Errorf("--from-json entry %d: condition and operator are required", i)
			}
		}
	} else {
		for _, raw := range condFlags {
			c, err := parseConditionSpec(raw)
			if err != nil {
				return nil, err
			}
			conds = append(conds, c)
		}
	}

	if joinAny {
		// Zotero stores joinMode as a leading pseudo-condition. The desktop
		// client always emits it first; mirror that placement for round-trip
		// stability.
		conds = append([]client.SearchCondition{{Condition: "joinMode", Operator: "any", Value: ""}}, conds...)
	}
	return conds, nil
}

// parseConditionSpec splits "field:operator:value" into a SearchCondition.
// Only the first two colons are honored — values may legitimately contain
// colons (e.g. "isInTheLast:30 days" is fine; "title:contains:foo:bar" yields
// value="foo:bar").
func parseConditionSpec(s string) (client.SearchCondition, error) {
	first := strings.IndexByte(s, ':')
	if first < 0 {
		return client.SearchCondition{}, fmt.Errorf("--condition %q: expected 'field:operator:value'", s)
	}
	rest := s[first+1:]
	second := strings.IndexByte(rest, ':')
	if second < 0 {
		return client.SearchCondition{}, fmt.Errorf("--condition %q: expected 'field:operator:value'", s)
	}
	cond := strings.TrimSpace(s[:first])
	op := strings.TrimSpace(rest[:second])
	val := rest[second+1:]
	if cond == "" || op == "" {
		return client.SearchCondition{}, fmt.Errorf("--condition %q: condition and operator must be non-empty", s)
	}
	return client.SearchCondition{Condition: cond, Operator: op, Value: val}, nil
}

// readSavedSearchJSON returns the bytes of the requested file, or stdin if
// path is "-".
func readSavedSearchJSON(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
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
