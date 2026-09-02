package cli

// The collection write verbs: create, update, delete, and the membership
// pair add / remove. Each one enters through requireAPIClient (write.go),
// resolves names against the local mirror, and then talks to the Zotero Web
// API. What comes back is the server's copy — the local mirror will not show
// the change until Zotero desktop syncs.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// collAddStdin is where `--from-file -` reads item keys from. A package
// variable so tests can inject a reader; production never reassigns it.
var collAddStdin io.Reader = os.Stdin

// collectionRefUsage is the shared explanation of what a collection
// positional accepts.
const collectionRefUsage = "A collection is named by its 8-character key or its exact name (case-insensitive)"

func collectionCreateCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a collection, top-level or under a parent",
		Description: "$ sci zot collection create \"Reading list\"\n" +
			"$ sci zot collection create Drafts --parent Projects\n" +
			"$ sci zot collection create Drafts --parent ABC12345\n\n" +
			"The parent is resolved against the local mirror by name; a key is\n" +
			"passed through verbatim, so a collection created seconds ago (not\n" +
			"yet in the mirror) still works as a parent.",
		ArgsUsage: "<name>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "parent", Usage: "nest under this collection (key or name)", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one collection name (quote a name with spaces)")
			}
			name := strings.TrimSpace(cmd.Args().First())
			if name == "" {
				return cmdutil.UsageErrorf(cmd, "expected a collection name")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			parentKey := ""
			if parentRef := cmd.String("parent"); parentRef != "" {
				parentKey, _, err = resolveCollectionArg(ctx, parentRef)
				if err != nil {
					return err
				}
			}
			obj, err := client.CreateCollection(ctx, name, parentKey)
			if err != nil {
				return writeErr(err)
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "created",
				Kind:    "collection",
				Target:  obj.Key,
				Message: fmt.Sprintf("created collection %s (%s)", name, obj.Key),
				Data:    obj,
			})
			return nil
		},
	}
}

func collectionUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Rename a collection or move it under another parent (or to the top level)",
		Description: "$ sci zot collection update \"Reading list\" --name \"To read\"\n" +
			"$ sci zot collection update ABC12345 --parent Projects\n" +
			"$ sci zot collection update ABC12345 --top-level\n\n" +
			"At least one of --name, --parent, --top-level is required;\n" +
			"--parent and --top-level exclude each other.\n" + collectionRefUsage + ".",
		ArgsUsage: "<key|name>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "new name", Local: true},
			&cli.StringFlag{Name: "parent", Usage: "move under this collection (key or name)", Local: true},
			&cli.BoolFlag{Name: "top-level", Usage: "move to the top level (no parent)", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one collection (key or name)")
			}
			newName := strings.TrimSpace(cmd.String("name"))
			parentRef := cmd.String("parent")
			topLevel := cmd.Bool("top-level")
			switch {
			case newName == "" && parentRef == "" && !topLevel:
				return cmdutil.UsageErrorf(cmd, "nothing to change: pass --name, --parent or --top-level")
			case parentRef != "" && topLevel:
				return cmdutil.UsageErrorf(cmd, "--parent and --top-level are mutually exclusive")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			collKey, _, err := resolveCollectionRef(db, cmd.Args().First())
			if err != nil {
				return err
			}
			patch := map[string]any{}
			if newName != "" {
				patch["name"] = newName
			}
			if topLevel {
				patch["parentCollection"] = false
			}
			if parentRef != "" {
				parentKey, _, err := resolveCollectionRef(db, parentRef)
				if err != nil {
					return err
				}
				if parentKey == collKey {
					return cmdutil.UsageErrorf(cmd, "a collection cannot be its own parent")
				}
				patch["parentCollection"] = parentKey
			}
			if err := client.PatchCollection(ctx, collKey, patch); err != nil {
				return writeErr(err)
			}
			updated, err := client.GetCollection(ctx, collKey)
			if err != nil {
				return writeErr(err)
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "updated",
				Kind:    "collection",
				Target:  collKey,
				Message: "updated collection " + serverCollectionLabel(updated),
				Data:    updated,
			})
			return nil
		},
	}
}

func collectionDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete",
		Usage: "Delete a collection (its items stay in the library; sub-collections go with it)",
		Description: "$ sci zot collection delete \"Old drafts\"\n" +
			"$ sci zot collection delete ABC12345 --yes\n\n" +
			"Asks for confirmation unless --yes is passed; under --json the\n" +
			"prompt cannot be shown, so --yes is required there.\n" + collectionRefUsage + ".",
		ArgsUsage: "<key|name>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return cmdutil.UsageErrorf(cmd, "expected exactly one collection (key or name)")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			collKey, coll, err := resolveCollectionRef(db, cmd.Args().First())
			if err != nil {
				return err
			}
			colls, err := db.ListCollections()
			if err != nil {
				return err
			}
			label := collectionLabel(coll, collKey)
			prompt := fmt.Sprintf("Delete collection %s? Items stay in the library; Zotero deletes its sub-collections with it.", label)
			if n := countSubCollections(colls, collKey); n > 0 {
				prompt = fmt.Sprintf("Delete collection %s and its %d sub-collection(s)? Items stay in the library; Zotero deletes the sub-collections with it.", label, n)
			}
			if done, err := cmdutil.ConfirmOrSkip(cmd.Bool("yes"), prompt); done || err != nil {
				return err
			}
			if err := client.DeleteCollection(ctx, collKey); err != nil {
				return writeErr(err)
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "deleted",
				Kind:    "collection",
				Target:  collKey,
				Message: "deleted collection " + label,
			})
			return nil
		},
	}
}

func collectionAddCommand() *cli.Command {
	return membershipCommand(membershipVerb{
		Name:        "add",
		Usage:       "File items into a collection",
		Action:      "added",
		Preposition: "to",
		Unchanged:   "is already in",
		Apply: func(ctx context.Context, c *api.Client, itemKey, collKey string) (bool, error) {
			return c.AddToCollection(ctx, itemKey, collKey)
		},
	})
}

func collectionRemoveCommand() *cli.Command {
	return membershipCommand(membershipVerb{
		Name:        "remove",
		Usage:       "Take items out of a collection (they stay in the library)",
		Action:      "removed",
		Preposition: "from",
		Unchanged:   "is not in",
		Apply: func(ctx context.Context, c *api.Client, itemKey, collKey string) (bool, error) {
			return c.RemoveFromCollection(ctx, itemKey, collKey)
		},
	})
}

// membershipVerb is what tells `collection add` and `collection remove`
// apart: the API call and the words in the messages. Everything else —
// positional parsing, --from-file, key resolution, the batch report — is
// shared by membershipCommand.
type membershipVerb struct {
	Name        string
	Usage       string
	Action      string // past participle for the result: "added" / "removed"
	Preposition string // "to" / "from"
	Unchanged   string // "is already in" / "is not in" — the no-op explanation
	Apply       func(ctx context.Context, c *api.Client, itemKey, collKey string) (changed bool, err error)
}

// membershipOutcome is one item's result in a membership batch.
type membershipOutcome struct {
	Key     string
	Changed bool
	Err     error
}

func membershipCommand(verb membershipVerb) *cli.Command {
	return &cli.Command{
		Name:  verb.Name,
		Usage: verb.Usage,
		Description: fmt.Sprintf("$ sci zot collection %[1]s ABC12345 \"Reading list\"\n"+
			"$ sci zot collection %[1]s ABC12345 DEF67890 smith2024-paper COLL1234\n"+
			"$ sci zot search '@tag: to-read' --json | jq -r '.data.items[].key' \\\n"+
			"    | sci zot collection %[1]s --from-file - \"Reading list\"\n\n"+
			"Every positional but the last is an item (8-character key or cite\n"+
			"key); the last is the collection. With --from-file the file (or\n"+
			"stdin, as -) carries one key per line — blank lines and # comments\n"+
			"are skipped — and the collection is the only positional. Every item\n"+
			"must resolve locally before anything is written: a cite key that\n"+
			"matches no item (or several) fails the whole command. Once writing\n"+
			"starts, items go one at a time, and an item the server refuses is\n"+
			"reported while the rest proceed.\n%[2]s.", verb.Name, collectionRefUsage),
		ArgsUsage: "<item…> <collection>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "from-file", Usage: "read item keys from this file, one per line (- for stdin)", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			itemArgs, collRef, err := membershipPositionals(cmd)
			if err != nil {
				return err
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			collKey, coll, err := resolveCollectionArg(ctx, collRef)
			if err != nil {
				return err
			}
			itemKeys, err := resolveItemArgs(ctx, itemArgs)
			if err != nil {
				return err
			}
			label := collectionLabel(coll, collKey)
			if coll == nil {
				// The mirror does not know this key, so it is either unsynced
				// or wrong. One server lookup settles it up front — a wrong
				// key is a single not-found, not one failure per item.
				remote, err := client.GetCollection(ctx, collKey)
				if err != nil {
					return writeErr(err)
				}
				label = serverCollectionLabel(remote)
			}
			// Serial on purpose: Zotero rate-limits, and the client's own
			// retry layer already honours its backoff signals.
			outcomes := lo.Map(itemKeys, func(key string, _ int) membershipOutcome {
				changed, err := verb.Apply(ctx, client, key, collKey)
				return membershipOutcome{Key: key, Changed: changed, Err: err}
			})
			if len(outcomes) == 1 {
				one := outcomes[0]
				if one.Err != nil {
					return writeErr(one.Err)
				}
				msg := fmt.Sprintf("%s item %s %s collection %s", verb.Action, one.Key, verb.Preposition, label)
				if !one.Changed {
					msg = fmt.Sprintf("item %s %s collection %s — nothing changed", one.Key, verb.Unchanged, label)
				}
				outputScoped(ctx, cmd, zot.WriteResult{Action: verb.Action, Kind: "item", Target: one.Key, Message: msg})
				return nil
			}
			outputScoped(ctx, cmd, bulkMembershipResult(verb.Action, outcomes))
			return nil
		},
	}
}

// membershipPositionals splits argv into the item references and the
// collection reference, honouring --from-file.
func membershipPositionals(cmd *cli.Command) (itemArgs []string, collRef string, err error) {
	args := cmd.Args().Slice()
	fromFile := cmd.String("from-file")
	if fromFile == "" {
		if len(args) < 2 {
			return nil, "", cmdutil.UsageErrorf(cmd, "expected one or more items followed by the collection")
		}
		return args[:len(args)-1], args[len(args)-1], nil
	}
	if len(args) != 1 {
		return nil, "", cmdutil.UsageErrorf(cmd, "with --from-file the only positional is the collection")
	}
	keys, err := readKeyList(fromFile)
	if err != nil {
		return nil, "", err
	}
	if len(keys) == 0 {
		return nil, "", cmdutil.Coded(cmdutil.CodeUsage, "no item keys in %s", describeKeySource(fromFile))
	}
	return keys, args[0], nil
}

// readKeyList reads one key per line from path (or stdin for "-"), dropping
// blank lines and # comments.
func readKeyList(path string) ([]string, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(collAddStdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", describeKeySource(path), err)
	}
	return lo.FilterMap(slices.Collect(strings.Lines(string(data))), func(line string, _ int) (string, bool) {
		line = strings.TrimSpace(line)
		return line, line != "" && !strings.HasPrefix(line, "#")
	}), nil
}

func describeKeySource(path string) string {
	if path == "-" {
		return "stdin"
	}
	return path
}

// resolveItemArgs turns item positionals into 8-character keys: a key is
// taken as is, anything else must resolve as a cite key against the local
// library. Never guesses — an ambiguous or unknown cite key is a usage
// error naming the argument.
func resolveItemArgs(ctx context.Context, args []string) ([]string, error) {
	return lo.MapErr(args, func(arg string, _ int) (string, error) {
		if zoteroKeyRE.MatchString(arg) {
			return arg, nil
		}
		if key := resolveCiteKeyArg(ctx, arg); key != "" {
			return key, nil
		}
		return "", cmdutil.Coded(cmdutil.CodeUsage,
			"%q is neither an 8-character item key nor a cite key this library resolves to exactly one item", arg).
			WithTry("find the key by title: sci zot search <title words>")
	})
}

// bulkMembershipResult folds a batch's outcomes into the report: every
// item that applied or was already in the wanted state counts as a
// success, failures carry the mapped error message, and the no-ops are
// listed under data.unchanged so a caller can tell the two apart.
func bulkMembershipResult(action string, outcomes []membershipOutcome) zot.BulkWriteResult {
	res := zot.BulkWriteResult{
		Action: action,
		Kind:   "item",
		Total:  len(outcomes),
		Success: lo.FilterMap(outcomes, func(o membershipOutcome, _ int) (string, bool) {
			return o.Key, o.Err == nil
		}),
	}
	failed := lo.Filter(outcomes, func(o membershipOutcome, _ int) bool { return o.Err != nil })
	if len(failed) > 0 {
		res.Failed = lo.SliceToMap(failed, func(o membershipOutcome) (string, string) {
			return o.Key, writeErr(o.Err).Error()
		})
	}
	unchanged := lo.FilterMap(outcomes, func(o membershipOutcome, _ int) (string, bool) {
		return o.Key, o.Err == nil && !o.Changed
	})
	if len(unchanged) > 0 {
		res.Data = map[string]any{"unchanged": unchanged}
	}
	return res
}

// resolveCollectionArg is resolveCollectionRef over a freshly opened local
// handle, for verbs that need exactly one lookup.
func resolveCollectionArg(ctx context.Context, ref string) (string, *local.Collection, error) {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = db.Close() }()
	return resolveCollectionRef(db, ref)
}

// resolveCollectionRef turns a user-supplied collection reference into a
// key, consulting the local mirror in this order:
//
//  1. an 8-character key the mirror has → that collection;
//  2. an exact name match, case-insensitive → that collection, or an
//     ambiguity error listing every candidate when several share the name;
//  3. an 8-character string matching nothing → taken verbatim as a key the
//     mirror has not synced yet (a collection created seconds ago), leaving
//     the server to answer 404 if it is wrong.
//
// The order matters because `^[A-Z0-9]{8}$` also matches names — PROJECTS,
// ARCHIVE1 — and a key-first rule would send such a name to the server as a
// key it never has, making the collection unaddressable by name. Nothing here
// guesses: a name with no match is not-found, several matches are
// ambiguous. coll is the mirror's row when it has one, for labels and counts.
func resolveCollectionRef(db local.Reader, ref string) (key string, coll *local.Collection, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, cmdutil.Coded(cmdutil.CodeUsage, "expected a collection key or name")
	}
	keyShaped := zoteroKeyRE.MatchString(ref)
	if keyShaped {
		coll, err := db.CollectionByKey(ref)
		if err != nil {
			return "", nil, err
		}
		if coll != nil {
			return ref, coll, nil
		}
	}
	colls, err := db.ListCollections()
	if err != nil {
		return "", nil, err
	}
	matches := lo.Filter(colls, func(c local.Collection, _ int) bool {
		return strings.EqualFold(c.Name, ref)
	})
	switch len(matches) {
	case 0:
		if keyShaped {
			return ref, nil, nil
		}
		return "", nil, cmdutil.Coded(cmdutil.CodeNotFound, "no collection named %q in this library", ref).
			WithTry("see names and keys with sci zot collection list, or pass the 8-character key (a collection created moments ago is not in the local mirror yet, but its key still works)")
	case 1:
		return matches[0].Key, &matches[0], nil
	default:
		candidates := lo.Map(matches, func(c local.Collection, _ int) string {
			if c.ParentKey == "" {
				return c.Key + " (top-level)"
			}
			return c.Key + " (under " + c.ParentKey + ")"
		})
		return "", nil, cmdutil.Coded(cmdutil.CodeAmbiguous, "%d collections are named %q: %s",
			len(matches), ref, strings.Join(candidates, ", ")).
			WithTry("re-run with the key of the one you mean")
	}
}

// collectionLabel names a collection for messages: "Name (KEY)" when the
// mirror knows it, the bare key otherwise.
func collectionLabel(coll *local.Collection, key string) string {
	if coll == nil {
		return key
	}
	return coll.Name + " (" + key + ")"
}

// serverCollectionLabel is collectionLabel for the server's copy — the one
// to use after a write, when the mirror's name may already be stale.
func serverCollectionLabel(obj *api.Object) string {
	name, _ := obj.Data["name"].(string)
	if name == "" {
		return obj.Key
	}
	return name + " (" + obj.Key + ")"
}

// countSubCollections counts every collection nested under key, at any
// depth, in the local mirror — what Zotero deletes along with it.
func countSubCollections(colls []local.Collection, key string) int {
	children := lo.GroupBy(colls, func(c local.Collection) string { return c.ParentKey })
	var walk func(parent string) int
	walk = func(parent string) int {
		n := len(children[parent])
		for _, child := range children[parent] {
			n += walk(child.Key)
		}
		return n
	}
	return walk(key)
}

// writeErr maps the api package's errors onto the coded vocabulary so every
// write verb reports the same way: a 404 is not-found, a 412 that survived
// the retry is a conflict, a per-object refusal is a runtime failure carrying
// Zotero's own message, and a 403 means the stored key cannot write here —
// which setup fixes. Anything else passes through untouched.
func writeErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, api.ErrNotFound) {
		return cmdutil.Coded(cmdutil.CodeNotFound, "not found on the Zotero server: %v", err).
			WithTry("keys are checked on the server, not the local mirror — confirm the key with sci zot collection list or sci zot item read <key>")
	}
	if conflict, ok := errors.AsType[*api.VersionConflictError](err); ok {
		return cmdutil.Coded(cmdutil.CodeConflict, "%s changed on the server while writing — re-run", conflict.Path)
	}
	if refused, ok := errors.AsType[*api.WriteFailedError](err); ok {
		return cmdutil.Coded(cmdutil.CodeRuntime, "Zotero rejected the write: %s", refused.Message)
	}
	if httpErr, ok := errors.AsType[*api.HTTPError](err); ok && httpErr.Status == http.StatusForbidden {
		// No Fix: the remedy carries a <key> placeholder, and a Fix must
		// be resubmittable verbatim.
		return cmdutil.Coded(cmdutil.CodeNotConfigured, "the API key cannot write to this library").
			WithTry(setupAPIKeyTry)
	}
	return err
}
