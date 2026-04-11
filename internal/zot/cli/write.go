package cli

import (
	"context"
	"fmt"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/urfave/cli/v3"
)

// Write-command flag destinations (package-scoped, matching sci-go conventions).
var (
	addType        string
	addTitle       string
	addDOI         string
	addURL         string
	addDate        string
	addAbstract    string
	addPublication string
	addAuthor      []string
	addCollection  string
	addTag         []string
	addExtra       string

	updTitle       string
	updDOI         string
	updURL         string
	updDate        string
	updAbstract    string
	updPublication string
	updExtra       string

	deleteYes bool

	collNewParent string

	tagRemoveYes bool
	tagDeleteYes bool
)

func writeCommands() []*cli.Command {
	return []*cli.Command{
		addCommand(),
		updateCommand(),
		deleteCommand(),
		collectionCommand(),
		tagCommand(),
	}
}

// requireAPIClient builds an API client from the loaded config, short-circuiting
// if the machine is offline or not configured.
func requireAPIClient() (*api.Client, error) {
	cfg, err := zot.RequireConfig()
	if err != nil {
		return nil, err
	}
	if !netutil.Online() {
		return nil, fmt.Errorf("no internet connection — zot writes require network access")
	}
	return api.New(cfg)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func addCommand() *cli.Command {
	return &cli.Command{
		Name:        "add",
		Usage:       "Create a new item in your Zotero library",
		Description: "$ zot add --type journalArticle --title \"My Paper\" --author \"Smith, Alice\" --doi 10.1000/abc",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "type", Value: "journalArticle", Usage: "item type (e.g. journalArticle, book, webpage)", Destination: &addType, Local: true},
			&cli.StringFlag{Name: "title", Usage: "item title (required)", Destination: &addTitle, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "DOI (no URL prefix)", Destination: &addDOI, Local: true},
			&cli.StringFlag{Name: "url", Usage: "URL", Destination: &addURL, Local: true},
			&cli.StringFlag{Name: "date", Usage: "publication date (freeform)", Destination: &addDate, Local: true},
			&cli.StringFlag{Name: "abstract", Usage: "abstract / summary", Destination: &addAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "journal / publication title", Destination: &addPublication, Local: true},
			&cli.StringSliceFlag{Name: "author", Usage: "author as \"Last, First\" (repeatable)", Destination: &addAuthor, Local: true},
			&cli.StringFlag{Name: "collection", Usage: "add item to collection key", Destination: &addCollection, Local: true},
			&cli.StringSliceFlag{Name: "tag", Usage: "attach a tag (repeatable)", Destination: &addTag, Local: true},
			&cli.StringFlag{Name: "extra", Usage: "free-text extra field (key: value lines)", Destination: &addExtra, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if addTitle == "" {
				return cmdutil.UsageErrorf(cmd, "--title is required")
			}
			c, err := requireAPIClient()
			if err != nil {
				return err
			}
			data := client.ItemData{
				ItemType: client.ItemDataItemType(addType),
				Title:    &addTitle,
			}
			data.DOI = strPtr(addDOI)
			data.Url = strPtr(addURL)
			data.Date = strPtr(addDate)
			data.AbstractNote = strPtr(addAbstract)
			data.PublicationTitle = strPtr(addPublication)
			data.Extra = strPtr(addExtra)

			if len(addAuthor) > 0 {
				creators := make([]client.Creator, 0, len(addAuthor))
				for _, a := range addAuthor {
					cr := parseCreator(a)
					creators = append(creators, cr)
				}
				data.Creators = &creators
			}
			if addCollection != "" {
				colls := []string{addCollection}
				data.Collections = &colls
			}
			if len(addTag) > 0 {
				tags := make([]client.Tag, 0, len(addTag))
				for _, t := range addTag {
					tags = append(tags, client.Tag{Tag: t})
				}
				data.Tags = &tags
			}

			key, err := c.CreateItem(ctx, data)
			if err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.WriteResult{
				Action: "created",
				Kind:   "item",
				Target: key,
			})
			return nil
		},
	}
}

// parseCreator parses a "Last, First" string into a client.Creator. Inputs
// without a comma are treated as single-name creators (institutions).
func parseCreator(s string) client.Creator {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			last := trim(s[:i])
			first := trim(s[i+1:])
			return client.Creator{CreatorType: "author", FirstName: &first, LastName: &last}
		}
	}
	name := trim(s)
	return client.Creator{CreatorType: "author", Name: &name}
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func updateCommand() *cli.Command {
	return &cli.Command{
		Name:        "update",
		Usage:       "Update fields on an existing item",
		Description: "$ zot update ABC12345 --title \"Corrected Title\"\n$ zot update ABC12345 --doi 10.1000/xyz",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Destination: &updTitle, Local: true},
			&cli.StringFlag{Name: "doi", Destination: &updDOI, Local: true},
			&cli.StringFlag{Name: "url", Destination: &updURL, Local: true},
			&cli.StringFlag{Name: "date", Destination: &updDate, Local: true},
			&cli.StringFlag{Name: "abstract", Destination: &updAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Destination: &updPublication, Local: true},
			&cli.StringFlag{Name: "extra", Destination: &updExtra, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()

			patch := client.ItemData{}
			any := false
			set := func(dst **string, v string) {
				if v != "" {
					*dst = strPtr(v)
					any = true
				}
			}
			set(&patch.Title, updTitle)
			set(&patch.DOI, updDOI)
			set(&patch.Url, updURL)
			set(&patch.Date, updDate)
			set(&patch.AbstractNote, updAbstract)
			set(&patch.PublicationTitle, updPublication)
			set(&patch.Extra, updExtra)
			if !any {
				return cmdutil.UsageErrorf(cmd, "at least one field flag is required")
			}

			c, err := requireAPIClient()
			if err != nil {
				return err
			}
			// itemType is required on the patch body — fetch current to supply it.
			cur, err := c.GetItemRaw(ctx, key)
			if err != nil {
				return err
			}
			patch.ItemType = cur.Data.ItemType

			if err := c.UpdateItem(ctx, key, patch); err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.WriteResult{
				Action: "updated",
				Kind:   "item",
				Target: key,
			})
			return nil
		},
	}
}

func deleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Aliases:     []string{"trash"},
		Usage:       "Move an item to trash",
		Description: "$ zot delete ABC12345\n$ zot delete ABC12345 --yes",
		ArgsUsage:   "<key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &deleteYes, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected an item key")
			}
			key := cmd.Args().First()
			if done, err := cmdutil.ConfirmOrSkip(deleteYes, fmt.Sprintf("Move item %s to trash?", key)); done || err != nil {
				return err
			}
			c, err := requireAPIClient()
			if err != nil {
				return err
			}
			if err := c.TrashItem(ctx, key); err != nil {
				return err
			}
			cmdutil.Output(cmd, zot.WriteResult{
				Action: "trashed",
				Kind:   "item",
				Target: key,
			})
			return nil
		},
	}
}

func collectionCommand() *cli.Command {
	return &cli.Command{
		Name:        "collection",
		Aliases:     []string{"coll"},
		Usage:       "Manage collections (create, delete, add/remove items)",
		Description: "$ zot collection create \"Brain Papers\"\n$ zot collection add ABC12345 COLLXXX1\n$ zot collection delete COLLXXX1",
		Commands: []*cli.Command{
			{
				Name:      "create",
				Usage:     "Create a new collection",
				ArgsUsage: "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "parent", Usage: "parent collection key", Destination: &collNewParent, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a collection name")
					}
					name := cmd.Args().First()
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					key, err := c.CreateCollection(ctx, name, collNewParent)
					if err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{Action: "created", Kind: "collection", Target: key,
						Message: fmt.Sprintf("created collection %q (%s)", name, key)})
					return nil
				},
			},
			{
				Name:      "delete",
				Usage:     "Delete a collection",
				ArgsUsage: "<key>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &deleteYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a collection key")
					}
					key := cmd.Args().First()
					if done, err := cmdutil.ConfirmOrSkip(deleteYes, fmt.Sprintf("Delete collection %s?", key)); done || err != nil {
						return err
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.DeleteCollection(ctx, key); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{Action: "deleted", Kind: "collection", Target: key})
					return nil
				},
			},
			{
				Name:      "add",
				Usage:     "Add an item to a collection",
				ArgsUsage: "<itemKey> <collectionKey>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <collectionKey>")
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.AddItemToCollection(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "added", Kind: "item", Target: args[0],
						Message: fmt.Sprintf("added item %s to collection %s", args[0], args[1]),
					})
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove an item from a collection",
				ArgsUsage: "<itemKey> <collectionKey>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <collectionKey>")
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.RemoveItemFromCollection(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "removed", Kind: "item", Target: args[0],
						Message: fmt.Sprintf("removed item %s from collection %s", args[0], args[1]),
					})
					return nil
				},
			},
		},
	}
}

func tagCommand() *cli.Command {
	return &cli.Command{
		Name:        "tag",
		Usage:       "Manage tags (add/remove per item, delete library-wide)",
		Description: "$ zot tag add ABC12345 neuroimaging\n$ zot tag remove ABC12345 deprecated\n$ zot tag delete deprecated",
		Commands: []*cli.Command{
			{
				Name:      "add",
				Usage:     "Attach a tag to an item",
				ArgsUsage: "<itemKey> <tag>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.AddTagToItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "added", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("added tag %q to item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:      "remove",
				Usage:     "Remove a tag from a single item",
				ArgsUsage: "<itemKey> <tag>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Destination: &tagRemoveYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.RemoveTagFromItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "removed", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("removed tag %q from item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:      "delete",
				Usage:     "Delete a tag from ALL items in the library",
				ArgsUsage: "<tag>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Destination: &tagDeleteYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a tag name")
					}
					tag := cmd.Args().First()
					if done, err := cmdutil.ConfirmOrSkip(tagDeleteYes,
						fmt.Sprintf("Delete tag %q from ALL items in the library?", tag)); done || err != nil {
						return err
					}
					c, err := requireAPIClient()
					if err != nil {
						return err
					}
					if err := c.DeleteTagsFromLibrary(ctx, []string{tag}); err != nil {
						return err
					}
					cmdutil.Output(cmd, zot.WriteResult{
						Action: "deleted", Kind: "tag", Target: tag,
						Message: fmt.Sprintf("deleted tag %q from library", tag),
					})
					return nil
				},
			},
		},
	}
}
