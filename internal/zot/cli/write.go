package cli

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/backfill"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/enrich"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/pkg/citekey"
	"github.com/urfave/cli/v3"
)

// collAddStdin is the stdin source for `zot collection add` when the user
// passes `-` or `--from-file -`. Overridable by tests.
var collAddStdin io.Reader = os.Stdin

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
	addCreator     []string
	addField       []string
	addCollection  string
	addTag         []string
	addExtra       string
	addOpenAlex    string

	updTitle       string
	updDOI         string
	updURL         string
	updDate        string
	updAbstract    string
	updPublication string
	updExtra       string
	updField       []string
	updType        string
	updAuthor      []string
	updCreator     []string
	updFromJSON    string

	deleteYes bool

	collNewParent   string
	collAddFromFile string
	collListRemote  bool

	tagRemoveYes bool
	tagDeleteYes bool
)

// requireAPIClient builds an API client from the loaded config, short-circuiting
// if the machine is offline or not configured. The library scope is resolved
// via ensureLibraryScope (auto-select / prompt / error per the holder set up
// by ValidateLibraryBefore).
func requireAPIClient(ctx context.Context) (*api.Client, error) {
	cfg, err := requireConfigCoded()
	if err != nil {
		return nil, err
	}
	if !netutil.Online() {
		return nil, cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — zot writes require network access").
			WithTry("re-run when online; local reads (search, item read/list, bib, export) work offline")
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if ref.Scope == zot.LibAll {
		return nil, cmdutil.Coded(cmdutil.CodeUsage,
			"--library all is local-read-only — API operations target one library").
			WithTry("re-run with --library personal or --library shared")
	}
	return api.New(cfg, api.WithLibrary(ref))
}

// buildItemPatch turns the --field flags into a PATCH body.
//
// Presence is read from cmd.IsSet, not from the value, because "leave this
// alone" and "empty this" are different instructions and a bare string
// cannot tell them apart. Before this there was no way to clear a field
// through the CLI at all, which surfaced on two items whose Extra held
// nothing but a `DOI-source:` line that a merge had made untrue.
//
// It deliberately does NOT use strPtr: that helper returns nil for an empty
// string, which is right for "the caller said nothing" and exactly wrong
// here. Going through it turned `--extra ""` into a patch carrying only
// key/version/itemType — a write that reports success and changes nothing,
// the same shape as the 709 empty patches that once reported
// "applied 709 of 709".
//
// --publication comes back SEPARATELY rather than on the patch, because its
// field name depends on the item's type (see applyVenue) and one patch may
// be applied to many keys of different types. `any` still counts it, so
// `--publication` alone is a valid edit.
func buildItemPatch(cmd *cli.Command) (patch client.ItemData, venue *string, any bool) {
	set := func(dst **string, flag string) {
		if !cmd.IsSet(flag) {
			return
		}
		v := cmd.String(flag)
		*dst = &v
		any = true
	}
	set(&patch.Title, "title")
	set(&patch.DOI, "doi")
	set(&patch.Url, "url")
	set(&patch.Date, "date")
	set(&patch.AbstractNote, "abstract")
	set(&venue, "publication")
	set(&patch.Extra, "extra")
	return patch, venue, any
}

// venueResolver is the slice of *api.Client that venue routing needs: given
// an item type, which field does Zotero's template say carries its venue.
type venueResolver interface {
	VenueField(ctx context.Context, itemType string) (string, error)
}

// itemTargeter adds the per-item type lookup `item update` needs on top of
// the schema readers — the same patch may be applied to a journal article
// and a book chapter in one call.
type itemTargeter interface {
	venueResolver
	itemSchema
	GetItem(ctx context.Context, key string) (*client.Item, error)
}

// updateSpec is everything `item update` resolved from its flags, kept
// together because almost every part of it depends on the item's type and
// therefore has to be applied per key rather than once per command.
type updateSpec struct {
	patch    client.ItemData
	venue    *string
	fields   []string
	authors  []string
	creators []string
	newType  string
}

// schemaBound reports whether anything in the spec needs the item's own
// type to place — i.e. whether the extra GET per key has to be paid.
func (s updateSpec) schemaBound() bool {
	return s.venue != nil || len(s.fields) > 0 ||
		len(s.authors) > 0 || len(s.creators) > 0 || s.newType != ""
}

// typeChange records what a --type does to one item, so the command can
// report it instead of letting the server discard fields in silence.
type typeChange struct {
	FromType string
	ToType   string
	// WillDrop is what the CLI predicts the new type cannot hold, computed
	// from the two schemas. The REPORTED loss is still a diff of two reads
	// (droppedFields) — this only decides what the patch clears.
	WillDrop []string
	// before is the item as it was read, kept so the post-write diff costs
	// no second GET of a state we already hold.
	before client.ItemData
}

// changed reports whether this is a real type change rather than --type
// naming the type the item already has.
func (t typeChange) changed() bool { return t.FromType != t.ToType }

// perItemPatches returns the patch to send for each key, identical to the
// shared one unless a schema-dependent flag was passed — in which case each
// key's values land in whatever fields ITS type declares.
//
// The extra GET per key is only paid when one of those flags is set, and it
// is the item's own type that decides: guessing from the first key would
// write a bookTitle onto a journal article and fail that whole item. Under
// --type the EFFECTIVE type is the new one, so the fields and creator types
// a repair introduces are checked against the type the item is becoming
// rather than the one it is leaving — otherwise setting a journalArticle's
// volume while promoting it from a `document` would need two commands and
// an invalid state in between.
//
// Creators REPLACE. A Zotero PATCH overwrites whole arrays, so a creator
// flag states the complete new list and anything not restated is gone. That
// is a sharp edge, and it is deliberate: the alternative — merging into the
// server's array — cannot express "this author is wrong, remove her", which
// is the repair creators are almost always needed for. An update naming no
// creator flag carries no creators array at all, so ordinary field edits
// are never a creator write.
func perItemPatches(ctx context.Context, c itemTargeter, keys []string, spec updateSpec) (map[string]client.ItemData, map[string]typeChange, error) {
	out := lo.SliceToMap(keys, func(k string) (string, client.ItemData) { return k, spec.patch })
	changes := map[string]typeChange{}
	if !spec.schemaBound() {
		return out, changes, nil
	}
	for _, k := range keys {
		it, err := c.GetItem(ctx, k)
		if err != nil {
			return nil, nil, err
		}
		curType := string(it.Data.ItemType)
		itemType := cmp.Or(spec.newType, curType)
		data := spec.patch

		if spec.newType != "" && spec.newType != curType {
			change, cerr := planTypeChange(ctx, c, it.Data, curType, spec.newType)
			if cerr != nil {
				return nil, nil, cerr
			}
			data.ItemType = client.ItemDataItemType(spec.newType)
			// Clear what the new type cannot hold IN THE SAME PATCH.
			// Zotero validates the item a patch RESULTS in, so leaving a
			// `publisher` on an item becoming a journalArticle is a failed
			// write rather than a degraded one — and one atomic patch
			// means a rejection changes nothing at all.
			for _, name := range change.WillDrop {
				api.SetField(&data, name, "")
			}
			changes[k] = change
		} else if spec.newType != "" {
			changes[k] = typeChange{FromType: curType, ToType: curType}
		}

		if spec.venue != nil {
			if err := applyVenue(ctx, c, &data, itemType, *spec.venue); err != nil {
				return nil, nil, err
			}
		}
		if err := applyFields(ctx, c, &data, itemType, spec.fields); err != nil {
			return nil, nil, err
		}
		if err := applyCreators(ctx, c, &data, itemType, spec.authors, spec.creators); err != nil {
			return nil, nil, err
		}
		out[k] = data
	}
	return out, changes, nil
}

// planTypeChange works out which of the item's populated fields the new
// type does not declare.
//
// Both schemas come from Zotero, so this states no opinion about which
// fields belong to which type; it only reads the two lists and subtracts.
func planTypeChange(ctx context.Context, s itemSchema, data client.ItemData, from, to string) (typeChange, error) {
	valid, err := s.ItemTypeFields(ctx, to)
	if err != nil {
		return typeChange{}, err
	}
	carried := api.FieldNames(data)
	drop := lo.Filter(carried, func(name string, _ int) bool {
		return !slices.Contains(valid, name)
	})
	return typeChange{FromType: from, ToType: to, WillDrop: drop, before: data}, nil
}

// validateItemType refuses a --type Zotero does not declare, before the
// command reads or writes anything.
//
// The round trip is not the reason: an invalid type answers 400 on the
// first item of a batch, by which time nothing has been written but the
// user has spent a write's worth of latency to learn they made a typo. The
// reason is that the fix is a spelling, so the message must carry the
// spellings — `journal-article` and `journalarticle` are both things people
// type, and neither is discoverable from the server's refusal.
func validateItemType(ctx context.Context, s itemSchema, itemType string) error {
	if itemType == "" {
		return nil
	}
	types, err := s.ItemTypes(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(types, itemType) {
		return nil
	}
	return cmdutil.Coded(cmdutil.CodeUsage, "%q is not a Zotero item type", itemType).
		WithTry("item types: " + strings.Join(types, ", "))
}

// updateReport builds the `data` an update returns, re-reading the item
// only when a type actually changed.
//
// The re-read is the whole point: what Zotero kept is a fact about the
// server, and predicting it from two schemas would report our model of the
// write rather than the write. Every other kind of edit changes exactly the
// fields it named, so it costs no round trip and returns nil.
func updateReport(ctx context.Context, c itemTargeter, key string, change typeChange, spec updateSpec) (*zot.ItemUpdateData, error) {
	data := &zot.ItemUpdateData{
		CreatorsReplaced: len(spec.authors) + len(spec.creators),
	}
	if change.changed() {
		after, err := c.GetItem(ctx, key)
		if err != nil {
			return nil, err
		}
		// Never nil: "the new type kept everything" is an answer, and a
		// JSON null there reads as "not computed".
		dropped := droppedFields(change.before, after.Data)
		if dropped == nil {
			dropped = []string{}
		}
		data.TypeChange = &zot.ItemTypeChange{
			From: change.FromType, To: change.ToType, DroppedFields: dropped,
		}
	}
	if data.TypeChange == nil && data.CreatorsReplaced == 0 {
		return nil, nil
	}
	return data, nil
}

// updateMessage renders the one line a human sees. A type change that threw
// fields away must say so on that line — a caller who has to ask for --json
// to discover a loss has already been surprised.
func updateMessage(key string, data *zot.ItemUpdateData) string {
	if data == nil || data.TypeChange == nil {
		return ""
	}
	msg := fmt.Sprintf("updated item %s (%s → %s)", key, data.TypeChange.From, data.TypeChange.To)
	if n := len(data.TypeChange.DroppedFields); n > 0 {
		msg += fmt.Sprintf("; dropped %d field(s): %s", n, strings.Join(data.TypeChange.DroppedFields, ", "))
	}
	return msg
}

// droppedFields names the fields an item carried before a write and does
// not carry after it.
//
// It is a diff of two READS rather than a replay of what the patch asked
// for, because the question it answers is what the server did — and on a
// type change the server discards fields nobody named. A field whose value
// merely changed is not a loss and would only bury the real casualties.
func droppedFields(before, after client.ItemData) []string {
	had := api.FieldNames(before)
	kept := api.FieldNames(after)
	return lo.Filter(had, func(name string, _ int) bool {
		return !slices.Contains(kept, name)
	})
}

// applyVenue places a --publication value in the field the item type
// actually declares — publicationTitle for a journal article, bookTitle for
// a book chapter, proceedingsTitle for a conference paper.
//
// Sending the wrong one is not a degraded write, it is a failed one:
// Zotero answers "'publicationTitle' is not a valid field for type
// 'bookSection'" and the whole request dies. A type with no venue field at
// all is bad input, so it is a usage error (exit 2) rather than a runtime
// error surfaced from the API after a round trip.
func applyVenue(ctx context.Context, r venueResolver, data *client.ItemData, itemType, value string) error {
	field, err := r.VenueField(ctx, itemType)
	if err != nil {
		return err
	}
	if field == "" {
		return cmdutil.Coded(cmdutil.CodeUsage,
			"--publication is not a valid field for item type %q", itemType).
			WithTry("only types with a container take one — use --type bookSection for a chapter in an edited volume, or --type conferencePaper for a proceedings paper")
	}
	return api.SetVenueField(data, field, value)
}

func addCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Create a new item in your Zotero library",
		// Every repeatable flag here carries a HUMAN NAME or a free-text
		// value, and urfave/cli splits slice values on comma by default.
		// That silently turned --author "Smith, Alice" into two creators,
		// "Smith" and " Alice", each of which parseCreator then read as an
		// INSTITUTIONAL author because neither half has a comma left.
		// Repeat the flag to pass several values.
		DisableSliceFlagSeparator: true,
		Description: "$ sci zot item add --type journalArticle --title \"My Paper\" --author \"Smith, Alice\" --doi 10.1000/abc\n" +
			"$ sci zot item add --type bookSection --title \"A Chapter\" --publication \"The Edited Volume\" \\\n" +
			"    --author \"Manning, Jeremy\" --creator \"editor:Gazzaniga, Michael\" \\\n" +
			"    --field publisher=\"MIT Press\" --field place=\"Cambridge, MA\" --field pages=45-70\n" +
			"$ sci zot item add --openalex 10.1038/nature12373\n" +
			"$ sci zot item add --openalex W2963403868 --collection ABC12345 --tag ml\n\n" +
			"--publication carries the VENUE, and Zotero names that field per item\n" +
			"type: publicationTitle on a journal article, bookTitle on a bookSection,\n" +
			"proceedingsTitle on a conferencePaper. The right one is chosen from the\n" +
			"type's own Zotero schema; a type with no venue field says so.\n\n" +
			"--field and --creator reach the rest of that schema — every field and\n" +
			"creator type the item type declares, checked before anything is sent.\n" +
			"Naming any creator replaces the set --openalex supplied.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "openalex", Usage: "lookup metadata on OpenAlex by DOI / W…-ID / arXiv / PMID", Destination: &addOpenAlex, Local: true},
			&cli.StringFlag{Name: "type", Value: "journalArticle", Usage: "item type (e.g. journalArticle, book, webpage)", Destination: &addType, Local: true},
			&cli.StringFlag{Name: "title", Usage: "item title (required unless --openalex)", Destination: &addTitle, Local: true},
			&cli.StringFlag{Name: "doi", Usage: "DOI (no URL prefix)", Destination: &addDOI, Local: true},
			&cli.StringFlag{Name: "url", Usage: "URL", Destination: &addURL, Local: true},
			&cli.StringFlag{Name: "date", Usage: "publication date (freeform)", Destination: &addDate, Local: true},
			&cli.StringFlag{Name: "abstract", Usage: "abstract / summary", Destination: &addAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "venue title — routed to the field the item type takes (publicationTitle / bookTitle / proceedingsTitle)", Destination: &addPublication, Local: true},
			&cli.StringSliceFlag{Name: "author", Usage: "author as \"Last, First\" (repeatable)", Destination: &addAuthor},                                                     // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{Name: "creator", Usage: "non-author creator as \"type:Last, First\", e.g. \"editor:Kahana, Michael\" (repeatable)", Destination: &addCreator}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{Name: "field", Usage: "any other field the item type takes, as name=value, e.g. --field pages=45-70 (repeatable)", Destination: &addField},    // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringFlag{Name: "collection", Usage: "add item to collection key", Destination: &addCollection, Local: true},
			&cli.StringSliceFlag{Name: "tag", Usage: "attach a tag (repeatable)", Destination: &addTag}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringFlag{Name: "extra", Usage: "free-text extra field (key: value lines)", Destination: &addExtra, Local: true},
		},
		Action: runAdd,
	}
}

func runAdd(ctx context.Context, cmd *cli.Command) error {
	data, err := buildAddItemData(ctx)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	// Everything schema-dependent is resolved once the client exists,
	// because the answers come from Zotero's own per-type schema. The type
	// is whatever survived applyAddFlagOverrides — --type when given, else
	// --openalex's guess.
	itemType := string(data.ItemType)
	if addPublication != "" {
		if err := applyVenue(ctx, c, &data, itemType, addPublication); err != nil {
			return err
		}
	}
	if err := applyCreators(ctx, c, &data, itemType, addAuthor, addCreator); err != nil {
		return err
	}
	if err := applyFields(ctx, c, &data, itemType, addField); err != nil {
		return err
	}
	it, err := c.CreateItem(ctx, data)
	if err != nil {
		return err
	}
	hydrated := api.ItemFromClient(it)
	citekey.Enrich(&hydrated)
	outputScoped(ctx, cmd, zot.WriteResult{
		Action: "created",
		Kind:   "item",
		Target: it.Key,
		Data:   hydrated,
	})
	return nil
}

// buildAddItemData composes the ItemData payload for `zot item add`. The
// --openalex path fetches + maps metadata, then manual flags overlay the
// result (so "--openalex W… --tag ml --collection XYZ" works as expected).
func buildAddItemData(ctx context.Context) (client.ItemData, error) {
	var data client.ItemData
	if addOpenAlex != "" {
		oa, err := openalexClient()
		if err != nil {
			return data, err
		}
		work, err := oa.ResolveWork(ctx, addOpenAlex)
		if err != nil {
			return data, fmt.Errorf("openalex lookup: %w", err)
		}
		data = enrich.ToItemFields(work)
	} else {
		if addTitle == "" {
			return data, fmt.Errorf("--title is required")
		}
		data = client.ItemData{
			ItemType: client.ItemDataItemType(addType),
			Title:    &addTitle,
		}
	}

	applyAddFlagOverrides(&data)
	return data, nil
}

// applyAddFlagOverrides lets explicit flags override any field already set by
// the --openalex mapping. Empty flags leave the mapped value untouched.
func applyAddFlagOverrides(data *client.ItemData) {
	if addType != "" && addType != "journalArticle" {
		// Only override itemType when the user explicitly changed it from the
		// default — otherwise --openalex's inference wins.
		data.ItemType = client.ItemDataItemType(addType)
	}
	if addTitle != "" {
		data.Title = &addTitle
	}
	if addDOI != "" {
		data.DOI = &addDOI
	}
	if addURL != "" {
		data.Url = &addURL
	}
	if addDate != "" {
		data.Date = &addDate
	}
	if addAbstract != "" {
		data.AbstractNote = &addAbstract
	}
	// --publication is deliberately NOT applied here: its field name
	// depends on the item type, which this function is still deciding.
	// runAdd places it once the type is settled (see applyVenue).
	if addExtra != "" {
		data.Extra = &addExtra
	}
	// Creators are NOT built here: --creator has to be validated against
	// the item type's schema, which needs a client. runAdd does both once
	// the type is settled (see applyCreators).
	if addCollection != "" {
		colls := []string{addCollection}
		data.Collections = &colls
	}
	if len(addTag) > 0 {
		tags := lo.Map(addTag, func(t string, _ int) client.Tag { return client.Tag{Tag: t} })
		data.Tags = &tags
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
		Name:  "update",
		Usage: "Update fields on one or more items",
		// See addCommand: --field values are free text and must not be
		// split on comma (`--field place="Cambridge, MA"`).
		DisableSliceFlagSeparator: true,
		Description: "$ sci zot item update ABC12345 --title \"Corrected Title\"\n" +
			"$ sci zot item update ABC12345 DEF67890 --publication \"Nature\"\n" +
			"$ sci zot item update ABC12345 --type journalArticle --publication \"Nature\" \\\n" +
			"    --author \"Gweon, Hyowon\" --author \"Fan, Judith\" --field volume=381\n" +
			"$ sci zot item update --from-json doi-backfill.ndjson\n" +
			"$ sci zot item update --from-json enrich-plan.ndjson\n" +
			"Providing multiple keys applies the same field patch to each item via a\n" +
			"batched POST /items request (up to 50 items per round-trip).\n\n" +
			"--author / --creator REPLACE the item's whole creator list: they state\n" +
			"the complete new set, and any creator not restated is removed. An\n" +
			"update naming neither leaves the creators untouched.\n\n" +
			"--type changes the item type. Zotero keeps only the fields the new type\n" +
			"declares, so the rest are removed and reported in data.type_change.\n" +
			"dropped_fields (a diff of the item before and after the write). Every\n" +
			"other flag is validated against the NEW type, so a document can become a\n" +
			"journalArticle and gain volume/issue/pages in one command.\n\n" +
			"--from-json applies MANY DISTINCT patches instead of one patch to many\n" +
			"keys. It reads either plan the zot binary writes: a DOI plan from `zot\n" +
			"backfill`, or a field plan from `zot enrich` (abstracts, volume, issue,\n" +
			"pages, PMID). Rows of both kinds may share one file.\n\n" +
			"Nothing is overwritten. A DOI plan composes Extra from the SERVER's copy\n" +
			"rather than the plan, so a note added on another device is not erased,\n" +
			"and an item that has gained a DOI since the plan was built is skipped. A\n" +
			"field plan checks EACH field against the server separately, so a volume\n" +
			"that appeared in the meantime costs that one field, not the other four.",
		ArgsUsage: "<key> [<key>...]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "title", Destination: &updTitle, Local: true},
			&cli.StringFlag{Name: "doi", Destination: &updDOI, Local: true},
			&cli.StringFlag{Name: "url", Destination: &updURL, Local: true},
			&cli.StringFlag{Name: "date", Destination: &updDate, Local: true},
			&cli.StringFlag{Name: "abstract", Destination: &updAbstract, Local: true},
			&cli.StringFlag{Name: "publication", Usage: "venue title — routed per item to the field ITS type takes", Destination: &updPublication, Local: true},
			&cli.StringFlag{Name: "extra", Destination: &updExtra, Local: true},
			&cli.StringFlag{Name: "type", Destination: &updType, Local: true,
				Usage: "change the item type (e.g. journalArticle) — fields the new type does not declare are REMOVED, and reported as dropped_fields"},
			&cli.StringSliceFlag{Name: "field", Usage: "any other field the item type takes, as name=value (repeatable)", Destination: &updField},                       // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{Name: "author", Usage: "author as \"Last, First\" (repeatable) — REPLACES the item's whole creator list", Destination: &updAuthor},     // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringSliceFlag{Name: "creator", Usage: "non-author creator as \"type:Last, First\" (repeatable) — REPLACES the whole list", Destination: &updCreator}, // lint:no-local — slice-flag Local quirk: see internal/zot/cli/sliceflag_quirk_test.go
			&cli.StringFlag{Name: "from-json", Destination: &updFromJSON, Local: true,
				Usage: "apply a patch plan (NDJSON from `zot backfill` or `zot enrich`)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keys := cmd.Args().Slice()
			if updFromJSON != "" {
				if len(keys) > 0 {
					return cmdutil.UsageErrorf(cmd,
						"--from-json carries its own keys; do not also pass them as arguments")
				}
				return runBackfillPlan(ctx, cmd)
			}
			if len(keys) == 0 {
				return cmdutil.UsageErrorf(cmd, "expected at least one item key")
			}

			patch, venue, anyField := buildItemPatch(cmd)
			spec := updateSpec{
				patch:    patch,
				venue:    venue,
				fields:   updField,
				authors:  updAuthor,
				creators: updCreator,
				newType:  updType,
			}
			if !anyField && !spec.schemaBound() {
				return cmdutil.UsageErrorf(cmd, "at least one field flag is required")
			}

			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}

			// Before any read or write: a mistyped --type is a spelling
			// the server cannot suggest, and on a batch its 400 would
			// arrive after earlier items were already patched.
			if err := validateItemType(ctx, c, updType); err != nil {
				return err
			}

			// One patch, many keys — but which fields a type accepts is
			// per TYPE, so --publication, --field, --creator and --type
			// are placed per key.
			perKey, changes, err := perItemPatches(ctx, c, keys, spec)
			if err != nil {
				return err
			}

			if len(keys) == 1 {
				// Fast path: single PATCH. UpdateItem fills in
				// ItemType internally if not supplied.
				if err := c.UpdateItem(ctx, keys[0], perKey[keys[0]]); err != nil {
					return err
				}
				data, err := updateReport(ctx, c, keys[0], changes[keys[0]], spec)
				if err != nil {
					return err
				}
				outputScoped(ctx, cmd, zot.WriteResult{
					Action: "updated", Kind: "item", Target: keys[0],
					Message: updateMessage(keys[0], data),
					Data:    data,
				})
				return nil
			}

			patches := lo.Map(keys, func(k string, _ int) api.ItemPatch {
				return api.ItemPatch{Key: k, Data: perKey[k]}
			})
			results, err := c.UpdateItemsBatch(ctx, patches)
			if err != nil {
				return err
			}
			var success []string
			failed := map[string]string{}
			for _, k := range keys {
				if e := results[k]; e != nil {
					failed[k] = e.Error()
				} else {
					success = append(success, k)
				}
			}
			// Only the items that actually applied are re-read: a failed
			// patch changed nothing, so it has nothing to report.
			reports := map[string]*zot.ItemUpdateData{}
			for _, k := range success {
				data, rerr := updateReport(ctx, c, k, changes[k], spec)
				if rerr != nil {
					return rerr
				}
				if data != nil {
					reports[k] = data
				}
			}
			out := zot.BulkWriteResult{
				Action:  "updated",
				Kind:    "item",
				Total:   len(keys),
				Success: success,
				Failed:  failed,
			}
			if len(reports) > 0 {
				out.Data = reports
			}
			outputScoped(ctx, cmd, out)
			return nil
		},
	}
}

func deleteCommand() *cli.Command {
	return &cli.Command{
		Name:        "delete",
		Aliases:     []string{"trash"},
		Usage:       "Move an item to trash",
		Description: "$ sci zot item delete ABC12345\n$ sci zot item delete ABC12345 --yes",
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
			c, err := requireAPIClient(ctx)
			if err != nil {
				return err
			}
			if err := c.TrashItem(ctx, key); err != nil {
				return err
			}
			outputScoped(ctx, cmd, zot.WriteResult{
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
		Usage:       "Manage collections (list, create, delete, add/remove items)",
		Description: "$ sci zot collection list\n$ sci zot collection create \"Brain Papers\"\n$ sci zot collection add ABC12345 COLLXXX1\n$ sci zot collection delete COLLXXX1",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every collection in the library with item counts",
				Description: "$ sci zot collection list\n$ sci zot collection list --remote   # bypass local SQLite, hit the Zotero Web API",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "remote", Usage: "fetch from the Zotero Web API (shows collections not yet synced locally)", Destination: &collListRemote, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if collListRemote {
						c, err := requireAPIClient(ctx)
						if err != nil {
							return err
						}
						raw, err := c.ListCollections(ctx)
						if err != nil {
							return err
						}
						colls := lo.Map(raw, func(c client.Collection, _ int) local.Collection {
							return api.CollectionFromClient(&c)
						})
						outputScoped(ctx, cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
						return nil
					}
					_, db, err := openLocalDB(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					colls, err := db.ListCollections()
					if err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.CollectionListResult{Count: len(colls), Collections: colls})
					return nil
				},
			},
			{
				Name:        "create",
				Usage:       "Create a new collection",
				Description: "$ sci zot collection create \"Brain Papers\"\n$ sci zot collection create \"Sub-topic\" --parent COLLXXX1",
				ArgsUsage:   "<name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "parent", Usage: "parent collection key", Destination: &collNewParent, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() == 0 {
						return cmdutil.UsageErrorf(cmd, "expected a collection name")
					}
					name := cmd.Args().First()
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					coll, err := c.CreateCollection(ctx, name, collNewParent)
					if err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action:  "created",
						Kind:    "collection",
						Target:  coll.Key,
						Message: fmt.Sprintf("created collection %q (%s)", name, coll.Key),
						Data:    api.CollectionFromClient(coll),
					})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a collection",
				Description: "$ sci zot collection delete COLLXXX1\n$ sci zot collection delete COLLXXX1 --yes",
				ArgsUsage:   "<key>",
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
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.DeleteCollection(ctx, key); err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{Action: "deleted", Kind: "collection", Target: key})
					return nil
				},
			},
			{
				Name:  "add",
				Usage: "Add one or many items to a collection",
				Description: "$ sci zot collection add ABC12345 COLLXXX1\n" +
					"$ sci zot collection add --from-file keys.txt COLLXXX1\n" +
					"$ cat keys.txt | zot collection add - COLLXXX1",
				ArgsUsage: "<itemKey> <collectionKey>  (or --from-file FILE <collectionKey>; '-' reads stdin)",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "from-file",
						Usage:       "read item keys from file (one per line, '#' comments); '-' reads stdin",
						Destination: &collAddFromFile,
						Local:       true,
					},
				},
				Action: runCollectionAdd,
			},
			{
				Name:        "remove",
				Usage:       "Remove an item from a collection",
				Description: "$ sci zot collection remove ABC12345 COLLXXX1",
				ArgsUsage:   "<itemKey> <collectionKey>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <collectionKey>")
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.RemoveItemFromCollection(ctx, args[0], args[1]); err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "removed", Kind: "item", Target: args[0],
						Message: fmt.Sprintf("removed item %s from collection %s", args[0], args[1]),
					})
					return nil
				},
			},
			collBrowseCommand(),
		},
	}
}

func tagsCommand() *cli.Command {
	return &cli.Command{
		Name:        "tags",
		Aliases:     []string{"tag"},
		Usage:       "Manage tags (list, add/remove per item, delete library-wide)",
		Description: "$ sci zot tags list\n$ sci zot tags add ABC12345 neuroimaging\n$ sci zot tags remove ABC12345 deprecated\n$ sci zot tags delete deprecated",
		Commands: []*cli.Command{
			{
				Name:        "list",
				Usage:       "List every tag in the library with usage counts",
				Description: "$ sci zot tags list",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					_, db, err := openLocalDB(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = db.Close() }()
					tags, err := db.ListTags()
					if err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.TagListResult{Count: len(tags), Tags: tags})
					return nil
				},
			},
			{
				Name:        "add",
				Usage:       "Attach a tag to an item",
				Description: "$ sci zot tags add ABC12345 neuroimaging",
				ArgsUsage:   "<itemKey> <tag>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.AddTagToItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "added", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("added tag %q to item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "remove",
				Usage:       "Remove a tag from a single item",
				Description: "$ sci zot tags remove ABC12345 deprecated\n$ sci zot tags remove ABC12345 deprecated --yes",
				ArgsUsage:   "<itemKey> <tag>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Destination: &tagRemoveYes, Local: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					args := cmd.Args().Slice()
					if len(args) != 2 {
						return cmdutil.UsageErrorf(cmd, "expected <itemKey> <tag>")
					}
					if done, err := cmdutil.ConfirmOrSkip(tagRemoveYes,
						fmt.Sprintf("Remove tag %q from item %s?", args[1], args[0])); done || err != nil {
						return err
					}
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.RemoveTagFromItem(ctx, args[0], args[1]); err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "removed", Kind: "tag", Target: args[1],
						Message: fmt.Sprintf("removed tag %q from item %s", args[1], args[0]),
					})
					return nil
				},
			},
			{
				Name:        "delete",
				Usage:       "Delete a tag from ALL items in the library",
				Description: "$ sci zot tags delete deprecated\n$ sci zot tags delete deprecated --yes\nRemoves the tag from every item in the library in one API call.",
				ArgsUsage:   "<tag>",
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
					c, err := requireAPIClient(ctx)
					if err != nil {
						return err
					}
					if err := c.DeleteTagsFromLibrary(ctx, []string{tag}); err != nil {
						return err
					}
					outputScoped(ctx, cmd, zot.WriteResult{
						Action: "deleted", Kind: "tag", Target: tag,
						Message: fmt.Sprintf("deleted tag %q from library", tag),
					})
					return nil
				},
			},
			tagsBrowseCommand(),
		},
	}
}

// runCollectionAdd handles both the single-item fast path and the bulk
// (--from-file / stdin) path. When many keys are supplied, we read the
// current collections + Version + ItemType from the local DB so
// UpdateItemsBatch can skip per-item GETs — a 2145-item run becomes ~43
// HTTP POSTs (batches of 50) instead of 4290 round-trips.
func runCollectionAdd(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	keys, collKey, err := resolveCollectionAddKeys(args, collAddFromFile, collAddStdin)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	// Single-item fast path: preserve the original <itemKey> <collectionKey>
	// shape so callers and scripts that use it keep working.
	if len(keys) == 1 && collAddFromFile == "" && args[0] != "-" {
		if err := c.AddItemToCollection(ctx, keys[0], collKey); err != nil {
			return err
		}
		outputScoped(ctx, cmd, zot.WriteResult{
			Action: "added", Kind: "item", Target: keys[0],
			Message: fmt.Sprintf("added item %s to collection %s", keys[0], collKey),
		})
		return nil
	}

	// Bulk path: load local snapshots for every requested key in one SQL
	// round-trip, merge collKey into each Item's Collections, batch-POST.
	// Any keys missing locally (common when the caller just created them
	// via the API and Zotero desktop hasn't synced yet) are fetched
	// individually from the Web API — correct at the cost of one GET per
	// miss; the common human case stays at zero API reads.
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	localItems, err := db.GetItemsByKeys(keys)
	if err != nil {
		return err
	}

	items, fallbackFailed := resolveBulkCollectionAddItems(
		keys, localItems,
		func(k string) (local.Item, error) {
			raw, gerr := c.GetItem(ctx, k)
			if gerr != nil {
				return local.Item{}, gerr
			}
			return api.ItemFromClient(raw), nil
		},
	)

	patches, alreadyMember := buildCollectionAddPatches(items, collKey)

	result := zot.BulkWriteResult{
		Action:  "added",
		Kind:    "item",
		Total:   len(keys),
		Success: slices.Clone(alreadyMember),
		Failed:  fallbackFailed,
	}

	if len(patches) > 0 {
		apiResults, err := c.UpdateItemsBatch(ctx, patches)
		if err != nil {
			return err
		}
		for _, p := range patches {
			if e := apiResults[p.Key]; e != nil {
				result.Failed[p.Key] = e.Error()
			} else {
				result.Success = append(result.Success, p.Key)
			}
		}
	}

	outputScoped(ctx, cmd, result)
	return nil
}

// resolveCollectionAddKeys decodes the argv shape into (itemKeys, collKey).
// Rules:
//   - 2 positionals, first != "-": single-item fast path, collKey = arg[1].
//   - 1 positional + --from-file: keys come from file (or stdin if path is "-").
//   - 2 positionals, first == "-": keys come from stdin, collKey = arg[1].
//   - mixing --from-file with a leading key positional is a usage error.
//   - empty input (after normalization) is a usage error.
func resolveCollectionAddKeys(args []string, fromFile string, stdin io.Reader) (keys []string, collKey string, err error) {
	switch {
	case fromFile != "" && len(args) == 1:
		collKey = args[0]
		src, closer, serr := openKeySource(fromFile, stdin)
		if serr != nil {
			return nil, "", serr
		}
		defer closer()
		keys, err = parseKeysFromReader(src)
	case fromFile != "" && len(args) != 1:
		return nil, "", fmt.Errorf("pass a single <collectionKey> positional when using --from-file (got %d)", len(args))
	case len(args) == 2 && args[0] == "-":
		collKey = args[1]
		keys, err = parseKeysFromReader(stdin)
	case len(args) == 2:
		return []string{args[0]}, args[1], nil
	default:
		return nil, "", fmt.Errorf("expected <itemKey> <collectionKey>, or --from-file FILE <collectionKey>")
	}
	if err != nil {
		return nil, "", err
	}
	if len(keys) == 0 {
		return nil, "", fmt.Errorf("no item keys provided")
	}
	return keys, collKey, nil
}

// openKeySource returns a reader for the requested file, or stdin if path
// is "-". The caller must invoke closer() when done.
func openKeySource(path string, stdin io.Reader) (io.Reader, func(), error) {
	if path == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %q: %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

// parseKeysFromReader reads item keys one per line, trimming whitespace,
// skipping blank lines and '#'-prefixed comments, and de-duplicating while
// preserving first-seen order. Suitable for piped doctor output and
// hand-edited lists alike.
func parseKeysFromReader(r io.Reader) ([]string, error) {
	var (
		out  []string
		seen = map[string]struct{}{}
		sc   = bufio.NewScanner(r)
	)
	// Zotero keys are 8 chars, but some pipelines might feed longer lines
	// (whole JSON records) — bump the buffer to avoid scanner truncation.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveBulkCollectionAddItems merges a local-DB snapshot with an API
// fallback, so `collection add --from-file` works for keys the local
// SQLite doesn't yet know about — typically items the same caller
// just created via the Web API before Zotero desktop synced them back.
//
// Local wins: keys already in `localItems` don't touch the API. Only
// keys missing from local are passed to getRemote (one call each —
// Zotero doesn't batch GETs by key across arbitrary keys without a
// server-side index we control). Per-key fetch errors land in `failed`
// so the caller can still POST a batch for the keys that did resolve.
func resolveBulkCollectionAddItems(
	keys []string,
	localItems []local.Item,
	getRemote func(key string) (local.Item, error),
) (items []local.Item, failed map[string]string) {
	failed = map[string]string{}
	have := lo.Keyify(lo.Map(localItems, func(it local.Item, _ int) string { return it.Key }))
	items = slices.Clone(localItems)
	for _, k := range keys {
		if _, ok := have[k]; ok {
			continue
		}
		it, err := getRemote(k)
		if err != nil {
			failed[k] = err.Error()
			continue
		}
		items = append(items, it)
	}
	return items, failed
}

// buildCollectionAddPatches splits local items into (needs-update, already-member).
// Items already in collKey produce no patch (zero API cost); the rest get a
// patch carrying Version + ItemType so UpdateItemsBatch's fast path avoids
// per-item GETs.
//
// The Version is not just a fast-path token — it is the safety interlock. The
// merged array is composed from the local mirror, which cannot see memberships
// added on the server since the last desktop sync, and a Zotero PATCH replaces
// `collections` wholesale. Submitting under the local version means any such
// membership makes the write 412 instead of silently erasing it; the Rebuild
// hook then re-runs the same union against the server's own array. Together
// they keep the zero-API-read fast path while making the stale case correct.
func buildCollectionAddPatches(items []local.Item, collKey string) (patches []api.ItemPatch, alreadyMember []string) {
	for _, it := range items {
		if slices.Contains(it.Collections, collKey) {
			alreadyMember = append(alreadyMember, it.Key)
			continue
		}
		merged := append(slices.Clone(it.Collections), collKey)
		patches = append(patches, api.ItemPatch{
			Key:      it.Key,
			Version:  it.Version,
			ItemType: it.Type,
			Data:     client.ItemData{Collections: &merged},
			Rebuild:  unionCollection(collKey),
		})
	}
	return patches, alreadyMember
}

// unionCollection returns the Rebuild hook for a collection-add patch: the
// same append, re-derived against whatever the server actually holds. Adding
// a collection the item already belongs to restates the server's array
// unchanged rather than duplicating the key, so a re-derive is idempotent.
func unionCollection(collKey string) func(*client.Item) (client.ItemData, error) {
	return func(cur *client.Item) (client.ItemData, error) {
		var current []string
		if cur.Data.Collections != nil {
			current = *cur.Data.Collections
		}
		merged := slices.Clone(current)
		if !slices.Contains(merged, collKey) {
			merged = append(merged, collKey)
		}
		return client.ItemData{Collections: &merged}, nil
	}
}

// runBackfillPlan applies a plan written by the zot binary — DOI rows from
// `zot backfill`, field rows from `zot enrich`, or both in one file.
//
// The plan is read and validated in full before a single write, so a bad
// row cannot leave the library half-patched in a state nobody planned.
//
// Rows are routed by the library each one names, not by --library. The
// corpus spans both and an item key is unique only within one, so a single
// scope makes the other library's keys 404 -- which reads as a broken plan
// rather than a misrouted write, and leaves half the backfill silently
// undone.
func runBackfillPlan(ctx context.Context, cmd *cli.Command) error {
	plans, err := backfill.Read(updFromJSON)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		outputScoped(ctx, cmd, backfill.CLIResult{Plan: updFromJSON})
		return nil
	}

	cfg, err := requireConfigCoded()
	if err != nil {
		return err
	}
	if !netutil.Online() {
		return cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — applying a plan requires network access")
	}

	total := &backfill.Result{}
	for _, scope := range []zot.LibraryScope{zot.LibPersonal, zot.LibShared} {
		rows := backfill.ByLibrary(plans)[string(scope)]
		if len(rows) == 0 {
			continue
		}
		ref, refErr := cfg.Resolve(scope)
		if refErr != nil {
			return refErr
		}
		c, clientErr := api.New(cfg, api.WithLibrary(ref))
		if clientErr != nil {
			return clientErr
		}
		res, applyErr := backfill.Apply(ctx, c, c, rows)
		if applyErr != nil {
			return applyErr
		}
		total.Merge(res)
	}

	outputScoped(ctx, cmd, backfill.CLIResult{
		Plan: updFromJSON, Planned: len(plans), Result: total,
	})
	return nil
}
