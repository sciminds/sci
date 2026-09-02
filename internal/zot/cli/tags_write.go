package cli

// `tags add` and `tags remove`: tag membership on one item, through the
// Web API. Each tag is its own derived PATCH (api.AddTag / RemoveTag), so a
// concurrent edit to the item's tag list is never overwritten.

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

func tagsAddCommand() *cli.Command {
	return tagsVerbCommand(tagsVerb{
		Name:      "add",
		Usage:     "Add tags to an item",
		Action:    "added",
		Unchanged: "already has",
		Apply: func(ctx context.Context, c *api.Client, itemKey, tag string) (bool, error) {
			return c.AddTag(ctx, itemKey, tag)
		},
	})
}

func tagsRemoveCommand() *cli.Command {
	return tagsVerbCommand(tagsVerb{
		Name:      "remove",
		Usage:     "Remove tags from an item (the tags themselves survive on other items)",
		Action:    "removed",
		Unchanged: "does not have",
		Apply: func(ctx context.Context, c *api.Client, itemKey, tag string) (bool, error) {
			return c.RemoveTag(ctx, itemKey, tag)
		},
	})
}

// tagsVerb is what tells `tags add` and `tags remove` apart.
type tagsVerb struct {
	Name      string
	Usage     string
	Action    string // "added" / "removed"
	Unchanged string // "already has" / "does not have" — the no-op explanation
	Apply     func(ctx context.Context, c *api.Client, itemKey, tag string) (changed bool, err error)
}

func tagsVerbCommand(verb tagsVerb) *cli.Command {
	return &cli.Command{
		Name:  verb.Name,
		Usage: verb.Usage,
		Description: fmt.Sprintf("$ sci zot tags %[1]s ABC12345 to-read\n"+
			"$ sci zot tags %[1]s smith2024-paper to-read methods \"social cognition\"\n\n"+
			"The first positional is the item, the rest are tags. One tag returns\n"+
			"a single result; several return a per-tag report whose data.unchanged\n"+
			"lists the no-ops. Tags are matched exactly (case included).\n%[2]s.", verb.Name, itemRefUsage),
		ArgsUsage: "<item> <tag…>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 2 {
				return cmdutil.UsageErrorf(cmd, "expected an item (key or cite key) followed by one or more tags")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			keys, err := resolveItemArgs(ctx, args[:1])
			if err != nil {
				return err
			}
			itemKey := keys[0]
			outcomes := lo.Map(args[1:], func(tag string, _ int) membershipOutcome {
				changed, err := verb.Apply(ctx, client, itemKey, tag)
				return membershipOutcome{Key: tag, Changed: changed, Err: err}
			})
			if len(outcomes) == 1 {
				one := outcomes[0]
				if one.Err != nil {
					return writeErr(one.Err)
				}
				msg := fmt.Sprintf("%s tag %q on item %s", verb.Action, one.Key, itemKey)
				if !one.Changed {
					msg = fmt.Sprintf("item %s %s tag %q — nothing changed", itemKey, verb.Unchanged, one.Key)
				}
				outputScoped(ctx, cmd, zot.WriteResult{Action: verb.Action, Kind: "tag", Target: one.Key, Message: msg})
				return nil
			}
			res := bulkMembershipResult(verb.Action, outcomes)
			res.Kind = "tag"
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}
