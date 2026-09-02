package cli

// The item write verbs that act on items the library already holds:
// update (field edits), delete (the trash, never the permanent delete) and
// attach (a file under an item). The field plumbing at the bottom —
// --author / --creator / --field parsing, venue routing, schema validation
// — is shared with item_add.go's manual mode. Each verb enters through
// requireAPIClient (write.go) and reports through writeErr.

import (
	"cmp"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// itemRefUsage is the shared explanation of what an item positional accepts.
const itemRefUsage = "An item is named by its 8-character key or a cite key the local library resolves to exactly one item"

func itemUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Edit fields of one or more items",
		Description: "$ sci zot item update ABC12345 --title \"Corrected title\" --date 2021\n" +
			"$ sci zot item update ABC12345 --field volume=12 --field issue=3\n" +
			"$ sci zot item update ABC12345 --author \"Smith, Alice\" --author \"Doe, Bob\"\n" +
			"$ sci zot item update ABC12345 DEF67890 --extra \"\"\n\n" +
			"Pass at least one field flag. An explicit empty value clears the\n" +
			"field (--extra \"\" sends extra: \"\"). --field name=value sets any\n" +
			"field the item's type declares (validated against Zotero's schema\n" +
			"before anything is sent); --publication goes to the type's venue\n" +
			"field (publicationTitle, bookTitle or proceedingsTitle).\n" +
			"--author / --creator REPLACE the whole creator list; --author \"\"\n" +
			"clears it. --type changes the item type: the fields the old type\n" +
			"held and the new one lacks are cleared in the same write (Zotero\n" +
			"rejects them otherwise) and listed in data.type_change.dropped_fields.\n" +
			"Several keys return a per-key report; one key returns the item as\n" +
			"the server holds it after the edit.\n" + itemRefUsage + ".",
		ArgsUsage: "<key…>",
		// Free-text values (--author "Smith, Alice") carry commas; without
		// this urfave splits them into two values.
		DisableSliceFlagSeparator: true,
		Flags: slices.Concat(
			dedicatedFieldFlags(updateFieldSpecs, ""),
			[]cli.Flag{
				&cli.StringFlag{Name: "publication", Usage: "venue: journal, book or proceedings title, routed by item type", Local: true},
				&cli.StringFlag{Name: "type", Usage: "change the item type (journalArticle, book, preprint, …); fields the new type lacks are cleared in the same write and reported as data.type_change.dropped_fields", Local: true},
			},
			creatorAndFieldFlags(""),
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected one or more item keys")
			}
			edits, err := readItemEdits(cmd, updateFieldSpecs)
			if err != nil {
				return err
			}
			if edits.empty() {
				return cmdutil.UsageErrorf(cmd, "nothing to change: pass at least one field flag (--title, --field name=value, --author, …)")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			keys, err := resolveItemArgs(ctx, cmd.Args().Slice())
			if err != nil {
				return err
			}
			outcomes := lo.Map(keys, func(key string, _ int) updateOutcome {
				data, err := updateItem(ctx, client, key, edits)
				return updateOutcome{Key: key, Data: data, Err: err}
			})
			if len(outcomes) == 1 {
				one := outcomes[0]
				if one.Err != nil {
					return one.Err
				}
				msg := "updated item " + serverItemLabel(one.Data.Item)
				if tc := one.Data.TypeChange; tc != nil {
					msg += fmt.Sprintf(" (%s → %s", tc.From, tc.To)
					if len(tc.DroppedFields) > 0 {
						msg += ", cleared " + strings.Join(tc.DroppedFields, ", ")
					}
					msg += ")"
				}
				outputScoped(ctx, cmd, zot.WriteResult{
					Action:  "updated",
					Kind:    "item",
					Target:  one.Key,
					Message: msg,
					Data:    one.Data,
				})
				return nil
			}
			res := zot.BulkWriteResult{
				Action: "updated",
				Kind:   "item",
				Total:  len(outcomes),
				Success: lo.FilterMap(outcomes, func(o updateOutcome, _ int) (string, bool) {
					return o.Key, o.Err == nil
				}),
			}
			failed := lo.Filter(outcomes, func(o updateOutcome, _ int) bool { return o.Err != nil })
			if len(failed) > 0 {
				res.Failed = lo.SliceToMap(failed, func(o updateOutcome) (string, string) {
					return o.Key, o.Err.Error()
				})
			}
			changes := lo.FilterMap(outcomes, func(o updateOutcome, _ int) (lo.Entry[string, *zot.ItemTypeChange], bool) {
				return lo.Entry[string, *zot.ItemTypeChange]{Key: o.Key, Value: o.Data.TypeChange}, o.Err == nil && o.Data.TypeChange != nil
			})
			if len(changes) > 0 {
				res.Data = map[string]any{"type_changes": lo.FromEntries(changes)}
			}
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}

// updateOutcome is one key's result in an update batch.
type updateOutcome struct {
	Key  string
	Data itemUpdateData
	Err  error
}

// itemUpdateData is what `item update` returns for one key: the item as
// the server holds it after the edit, and — only when --type changed it —
// what the type change cleared.
type itemUpdateData struct {
	Item       *api.Object         `json:"item"`
	TypeChange *zot.ItemTypeChange `json:"type_change,omitempty"`
}

// updateItem validates edits against the item's (possibly new) type,
// patches it, and returns the server's copy afterwards. A type change
// clears, in the same PATCH, every non-empty field the old type declared
// and the new one does not: Zotero refuses a payload carrying a field the
// type lacks, and leaving them in place is exactly that.
func updateItem(ctx context.Context, client *api.Client, key string, edits itemEdits) (itemUpdateData, error) {
	cur, err := client.GetItem(ctx, key)
	if err != nil {
		return itemUpdateData{}, writeErr(err)
	}
	curType, _ := cur.Data["itemType"].(string)
	itemType := cmp.Or(edits.Type, curType)
	patch, err := edits.apply(ctx, client, itemType)
	if err != nil {
		return itemUpdateData{}, err
	}
	var change *zot.ItemTypeChange
	if edits.Type != "" && edits.Type != curType {
		dropped, err := fieldsLostByTypeChange(ctx, client, cur, curType, edits.Type)
		if err != nil {
			return itemUpdateData{}, err
		}
		for _, f := range dropped {
			patch[f] = ""
		}
		patch["itemType"] = edits.Type
		change = &zot.ItemTypeChange{From: curType, To: edits.Type, DroppedFields: dropped}
	}
	if err := client.PatchItem(ctx, key, patch); err != nil {
		return itemUpdateData{}, writeErr(err)
	}
	after, err := client.GetItem(ctx, key)
	if err != nil {
		return itemUpdateData{}, writeErr(err)
	}
	return itemUpdateData{Item: after, TypeChange: change}, nil
}

// fieldsLostByTypeChange lists the fields cur holds a non-empty value for
// that from declares and to does not, in from's schema order.
func fieldsLostByTypeChange(ctx context.Context, client *api.Client, cur *api.Object, from, to string) ([]string, error) {
	oldFields, err := client.ItemTypeFields(ctx, from)
	if err != nil {
		return nil, schemaErr(from, err)
	}
	newFields, err := client.ItemTypeFields(ctx, to)
	if err != nil {
		return nil, schemaErr(to, err)
	}
	keep := lo.Keyify(newFields)
	return lo.Filter(oldFields, func(f string, _ int) bool {
		if _, ok := keep[f]; ok {
			return false
		}
		value, _ := cur.Data[f].(string)
		return value != ""
	}), nil
}

func itemDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:    "delete",
		Aliases: []string{"trash"},
		Usage:   "Move items to the Zotero trash (restore or empty it from Zotero desktop)",
		Description: "$ sci zot item delete ABC12345\n" +
			"$ sci zot item trash ABC12345 smith2024-paper --yes\n\n" +
			"Never a permanent delete: the item goes to Zotero's trash, where\n" +
			"the desktop app restores or empties it. Asks for confirmation\n" +
			"unless --yes is passed; under --json the prompt cannot be shown, so\n" +
			"--yes is required there. An item already in the trash is a success\n" +
			"that changed nothing.\n" + itemRefUsage + ".",
		ArgsUsage: "<key…>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip the confirmation prompt", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected one or more item keys")
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			keys, err := resolveItemArgs(ctx, cmd.Args().Slice())
			if err != nil {
				return err
			}
			labels := localItemLabels(ctx, keys)
			prompt := fmt.Sprintf("Move %d item(s) to the Zotero trash? %s", len(keys), strings.Join(labels, "; "))
			if done, err := cmdutil.ConfirmOrSkip(cmd.Bool("yes"), prompt); done || err != nil {
				return err
			}
			outcomes := lo.Map(keys, func(key string, _ int) membershipOutcome {
				changed, err := client.TrashItem(ctx, key)
				return membershipOutcome{Key: key, Changed: changed, Err: err}
			})
			if len(outcomes) == 1 {
				one := outcomes[0]
				if one.Err != nil {
					return writeErr(one.Err)
				}
				res := zot.WriteResult{Action: "trashed", Kind: "item", Target: one.Key,
					Message: "moved item " + labels[0] + " to the trash"}
				if !one.Changed {
					res.Action = "unchanged"
					res.Message = "item " + labels[0] + " is already in the trash — nothing changed"
				}
				outputScoped(ctx, cmd, res)
				return nil
			}
			outputScoped(ctx, cmd, bulkMembershipResult("trashed", outcomes))
			return nil
		},
	}
}

// localItemLabels names items for a prompt: "Title (KEY)" when the local
// mirror knows the item, the bare key otherwise. Best effort — a mirror
// that cannot be opened or lacks the row costs nothing but the title.
func localItemLabels(ctx context.Context, keys []string) []string {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return keys
	}
	defer func() { _ = db.Close() }()
	return lo.Map(keys, func(key string, _ int) string {
		it, err := db.Read(key)
		if err != nil || it.Title == "" {
			return key
		}
		return fmt.Sprintf("%q (%s)", it.Title, key)
	})
}

func itemAttachCommand() *cli.Command {
	return &cli.Command{
		Name:  "attach",
		Usage: "Upload a file as a child attachment of an item",
		Description: "$ sci zot item attach ABC12345 paper.pdf\n" +
			"$ sci zot item attach smith2024-paper paper.pdf --skip-existing\n\n" +
			"Creates an imported-file attachment under the item and uploads the\n" +
			"bytes. A plain attach is not idempotent: run twice, it attaches the\n" +
			"file twice. --skip-existing hashes the file first and does nothing\n" +
			"when the item already has an attachment with that md5 (checked on\n" +
			"the server, not the local mirror). data.uploaded is false when\n" +
			"Zotero already held the bytes and skipped the transfer.\n" + itemRefUsage + ".",
		ArgsUsage: "<key> <file>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "skip-existing", Usage: "do nothing when the item already has an attachment with this file's md5", Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 2 {
				return cmdutil.UsageErrorf(cmd, "expected an item (key or cite key) followed by one file")
			}
			path := cmd.Args().Get(1)
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				return cmdutil.UsageErrorf(cmd, "%s is not a readable file", path)
			}
			client, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			keys, err := resolveItemArgs(ctx, []string{cmd.Args().First()})
			if err != nil {
				return err
			}
			itemKey := keys[0]
			if cmd.Bool("skip-existing") {
				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := md5.Sum(content)
				has, err := client.HasAttachmentWithMD5(ctx, itemKey, hex.EncodeToString(sum[:]))
				if err != nil {
					return writeErr(err)
				}
				if has {
					outputScoped(ctx, cmd, zot.WriteResult{Action: "unchanged", Kind: "attachment", Target: itemKey,
						Message: fmt.Sprintf("%s is already attached to item %s — nothing changed", filepath.Base(path), itemKey)})
					return nil
				}
			}
			att, uploaded, err := attachFile(ctx, client, itemKey, path)
			if err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "attached",
				Kind:    "attachment",
				Target:  itemKey,
				Message: fmt.Sprintf("attached %s to item %s (%s)", filepath.Base(path), itemKey, att.Key),
				Data:    map[string]any{"attachment": att, "attachment_key": att.Key, "uploaded": uploaded},
			})
			return nil
		},
	}
}

// attachFile creates an imported-file child attachment under parentKey and
// uploads the file's bytes — Zotero's four-phase dance, in one call.
// uploaded is false when the server already held a file with the same md5
// and skipped the transfer. The error is already mapped through writeErr.
//
// An upload that fails after the attachment item was created would leave
// an empty imported_file child on the server — invisible from here, since
// the local mirror lags — so the attachment is moved to the trash (best
// effort) and the error says so. Nothing usable survives a failure.
func attachFile(ctx context.Context, client *api.Client, parentKey, path string) (att *api.Object, uploaded bool, err error) {
	name := filepath.Base(path)
	contentType := contentTypeFor(path)
	att, err = client.CreateChildAttachment(ctx, parentKey, api.AttachmentMeta{Filename: name, ContentType: contentType, Title: name})
	if err != nil {
		return nil, false, writeErr(err)
	}
	uploaded, err = uploadAttachment(ctx, client, att.Key, path, name, contentType)
	if err != nil {
		if _, trashErr := client.TrashItem(ctx, att.Key); trashErr != nil {
			return nil, false, writeErr(fmt.Errorf("upload %s: %w; the empty attachment %s could not be trashed either (%v) — remove it in Zotero", name, err, att.Key, trashErr))
		}
		return nil, false, writeErr(fmt.Errorf("upload %s: %w; the empty attachment %s was moved to the trash", name, err, att.Key))
	}
	return att, uploaded, nil
}

// uploadAttachment streams path's bytes to an attachment item.
func uploadAttachment(ctx context.Context, client *api.Client, attKey, path, name, contentType string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	return client.UploadAttachmentFile(ctx, attKey, f, name, contentType)
}

// contentTypeFor is the MIME type an attachment is registered with: PDFs
// are the common case and named outright; anything else goes by extension.
func contentTypeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".pdf" {
		return "application/pdf"
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}
	return "application/octet-stream"
}

// serverItemLabel names an item from the server's copy: "Title (KEY)" when
// it has a title, the bare key otherwise.
func serverItemLabel(obj *api.Object) string {
	title, _ := obj.Data["title"].(string)
	if title == "" {
		return obj.Key
	}
	return fmt.Sprintf("%q (%s)", title, obj.Key)
}

// --- field plumbing shared by item add (manual mode) and item update ------

// fieldFlagSpec pairs a dedicated flag with the Zotero field it sets.
type fieldFlagSpec struct {
	Flag  string
	Field string
	Usage string
}

// updateFieldSpecs are item update's dedicated field flags.
var updateFieldSpecs = []fieldFlagSpec{
	{Flag: "title", Field: "title", Usage: "title"},
	{Flag: "doi", Field: "DOI", Usage: "DOI (bare 10.x/y form)"},
	{Flag: "url", Field: "url", Usage: "URL"},
	{Flag: "date", Field: "date", Usage: "date (YYYY, YYYY-MM or YYYY-MM-DD)"},
	{Flag: "abstract", Field: "abstractNote", Usage: "abstract"},
	{Flag: "extra", Field: "extra", Usage: "the extra field"},
}

// addFieldSpecs are item add's dedicated field flags in manual mode — the
// same set minus --doi, because an identifier is what the positional form
// resolves and a hand-made record has none.
var addFieldSpecs = lo.Filter(updateFieldSpecs, func(s fieldFlagSpec, _ int) bool { return s.Flag != "doi" })

// venueFields are the Zotero fields a venue can land in, in preference
// order; the first one the item type declares wins. Mirrors csl's routing.
var venueFields = []string{"publicationTitle", "bookTitle", "proceedingsTitle"}

// dedicatedFieldFlags renders specs as string flags; usagePrefix opens
// each Usage ("manual mode: " on item add, "" on update).
func dedicatedFieldFlags(specs []fieldFlagSpec, usagePrefix string) []cli.Flag {
	return lo.Map(specs, func(s fieldFlagSpec, _ int) cli.Flag {
		return &cli.StringFlag{Name: s.Flag, Usage: usagePrefix + s.Usage, Local: true}
	})
}

// creatorAndFieldFlags are the three free-text slice flags; usagePrefix
// ("manual mode: " on item add) opens each Usage. No Local: a slice flag
// with Local keeps only its last occurrence (lint-guard rule 17,
// internal/zot/CLAUDE.md "Slice-flag Local quirk").
func creatorAndFieldFlags(usagePrefix string) []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "author", Usage: usagePrefix + "an author as \"Last, First\" (no comma: a single-field institutional name); repeatable; \"\" clears the creators"}, // lint:no-local — slice flag
		&cli.StringSliceFlag{Name: "creator", Usage: usagePrefix + "a creator of another role as \"type:Last, First\" (editor:Doe, Jane); repeatable"},                                // lint:no-local — slice flag
		&cli.StringSliceFlag{Name: "field", Usage: usagePrefix + "any other field as name=value (volume=12); validated against the item type; repeatable"},                            // lint:no-local — slice flag
	}
}

// itemEdits is one command's field edits, read off the flags and not yet
// bound to an item type: the type decides which fields exist and where the
// venue goes, so apply does that part.
type itemEdits struct {
	// Type is --type, or "" when the type is not being changed.
	Type string
	// Fields maps Zotero field name → value for the dedicated flags and
	// --field entries. An explicit empty value is kept: it clears the field.
	Fields map[string]string
	// Publication is --publication; Set tells "" (clear) from absent.
	Publication    string
	PublicationSet bool
	// Creators is the full replacement list from --author / --creator, or
	// nil when neither flag appeared.
	Creators []map[string]any
}

func (e itemEdits) empty() bool {
	return e.Type == "" && len(e.Fields) == 0 && !e.PublicationSet && e.Creators == nil
}

// readItemEdits parses the field flags. Everything that needs no schema is
// checked here so a malformed flag is a usage error before any request: a
// --field without `=`, and a --field naming something a dedicated flag owns
// (--field title=X must be --title X, or two flags describe one field).
func readItemEdits(cmd *cli.Command, specs []fieldFlagSpec) (itemEdits, error) {
	edits := itemEdits{Type: cmd.String("type"), Fields: map[string]string{}}
	for _, s := range specs {
		if cmd.IsSet(s.Flag) {
			edits.Fields[s.Field] = cmd.String(s.Flag)
		}
	}
	if cmd.IsSet("publication") {
		edits.Publication, edits.PublicationSet = cmd.String("publication"), true
	}
	owned := lo.SliceToMap(specs, func(s fieldFlagSpec) (string, string) { return s.Field, "--" + s.Flag })
	owned["itemType"] = "--type"
	for _, v := range venueFields {
		owned[v] = "--publication"
	}
	for _, raw := range cmd.StringSlice("field") {
		name, value, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return itemEdits{}, cmdutil.UsageErrorf(cmd, "--field %q: expected name=value", raw)
		}
		if flag, taken := owned[name]; taken {
			return itemEdits{}, cmdutil.UsageErrorf(cmd, "--field %s: that field has its own flag, use %s", name, flag)
		}
		edits.Fields[name] = value
	}
	creators, err := parseCreatorFlags(cmd)
	if err != nil {
		return itemEdits{}, err
	}
	edits.Creators = creators
	return edits, nil
}

// parseCreatorFlags reads --author and --creator into Zotero creator
// objects, authors first. nil when neither flag appeared, so a caller can
// tell "leave the creators alone" from "clear them": an empty --author ""
// contributes nothing, and a list that ends up empty is sent as an empty
// list — the clear.
func parseCreatorFlags(cmd *cli.Command) ([]map[string]any, error) {
	if !cmd.IsSet("author") && !cmd.IsSet("creator") {
		return nil, nil
	}
	authors := lo.FilterMap(cmd.StringSlice("author"), func(name string, _ int) (map[string]any, bool) {
		return creatorEntry("author", name), strings.TrimSpace(name) != ""
	})
	others, err := lo.MapErr(cmd.StringSlice("creator"), func(raw string, _ int) (map[string]any, error) {
		role, name, ok := strings.Cut(raw, ":")
		role = strings.TrimSpace(role)
		if !ok || role == "" || strings.TrimSpace(name) == "" {
			return nil, cmdutil.UsageErrorf(cmd, "--creator %q: expected type:Last, First (editor:Doe, Jane)", raw)
		}
		return creatorEntry(role, name), nil
	})
	if err != nil {
		return nil, err
	}
	creators := slices.Concat(authors, others)
	if creators == nil {
		creators = []map[string]any{}
	}
	return creators, nil
}

// creatorEntry renders one name as a Zotero creator: "Last, First" becomes
// a two-field name, anything without a comma a single-field (institutional)
// one.
func creatorEntry(role, name string) map[string]any {
	last, first, ok := strings.Cut(name, ",")
	if !ok {
		return map[string]any{"creatorType": role, "name": strings.TrimSpace(name)}
	}
	return map[string]any{"creatorType": role, "lastName": strings.TrimSpace(last), "firstName": strings.TrimSpace(first)}
}

// apply binds the edits to an item type: every field must be one the type
// declares, the venue goes to the type's venue field (or is refused when it
// has none), and every creator role must be one the type accepts. Nothing
// is sent until all of it checks out. The returned payload carries no
// itemType; callers add it when they change the type.
func (e itemEdits) apply(ctx context.Context, client *api.Client, itemType string) (map[string]any, error) {
	fields, err := client.ItemTypeFields(ctx, itemType)
	if err != nil {
		return nil, schemaErr(itemType, err)
	}
	has := lo.Keyify(fields)
	data := map[string]any{}
	for _, name := range slices.Sorted(maps.Keys(e.Fields)) {
		if _, ok := has[name]; !ok {
			return nil, cmdutil.Coded(cmdutil.CodeUsage, "item type %s has no field %q", itemType, name).
				WithTry("its fields are: " + strings.Join(fields, ", "))
		}
		data[name] = e.Fields[name]
	}
	if e.PublicationSet {
		venue, ok := lo.Find(venueFields, func(f string) bool { _, ok := has[f]; return ok })
		if !ok {
			return nil, cmdutil.Coded(cmdutil.CodeUsage, "item type %s has no venue field, so --publication does not apply", itemType).
				WithTry("its fields are: " + strings.Join(fields, ", "))
		}
		data[venue] = e.Publication
	}
	if e.Creators != nil {
		roles, err := client.ItemTypeCreatorTypes(ctx, itemType)
		if err != nil {
			return nil, schemaErr(itemType, err)
		}
		allowed := lo.Keyify(roles)
		for _, c := range e.Creators {
			role, _ := c["creatorType"].(string)
			if _, ok := allowed[role]; !ok {
				return nil, cmdutil.Coded(cmdutil.CodeUsage, "item type %s has no creator type %q", itemType, role).
					WithTry("its creator types are: " + strings.Join(roles, ", "))
			}
		}
		data["creators"] = e.Creators
	}
	return data, nil
}

// schemaErr maps a schema lookup failure: Zotero answers 400 for an item
// type it does not know, which is a usage error naming the type; anything
// else is the transport's problem.
func schemaErr(itemType string, err error) error {
	if httpErr, ok := errors.AsType[*api.HTTPError](err); ok && httpErr.Status == http.StatusBadRequest {
		return cmdutil.Coded(cmdutil.CodeUsage, "unknown item type %q", itemType).
			WithTry("Zotero item types are camelCase: journalArticle, book, bookSection, conferencePaper, preprint, thesis, report, webpage, document, …")
	}
	return writeErr(err)
}

// itemTitleFromMirror is the local mirror's title for key, or "" when the
// mirror lacks the row.
func itemTitleFromMirror(db local.Reader, key string) string {
	it, err := db.Read(key)
	if err != nil {
		return ""
	}
	return it.Title
}
