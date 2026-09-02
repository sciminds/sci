package cli

// `sci zot item add`: put a paper into the library from an identifier, a
// PDF, a directory of PDFs, or hand-typed fields.
//
// The identifier is what everything resolves through. A DOI or arXiv id
// goes to doi.org (internal/zot/doiorg) for its registry record; a PDF is
// read for the identifier printed on its first pages (internal/zot/pdfid)
// and then takes the same road. The record becomes item data through
// internal/zot/csl, is checked against the local mirror for an item that
// already carries the DOI, and is created through the Web API; a PDF is
// then uploaded under the new item. Manual mode (--title) skips the
// resolution and builds the item from flags, sharing item_write.go's field
// plumbing with item update.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/local"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/internal/zot/csl"
	"github.com/sciminds/sci/internal/zot/doiorg"
	"github.com/sciminds/sci/internal/zot/pdfid"
	"github.com/urfave/cli/v3"
)

// newDOIResolver builds the doi.org client; tests swap in one pointed at a
// fake. Package-global, so tests that swap it must not run in parallel.
var newDOIResolver = doiorg.New

// sniffPDF reads a PDF's identifiers; tests replace it so the suite needs
// neither poppler nor real PDFs.
var sniffPDF = pdfid.Sniff

// bareArxivRE matches an arXiv id typed without its prefix (1706.03762,
// optionally versioned). bib.ScanText only recognises the arXiv: form,
// which is right for prose and wrong for a command-line argument.
var bareArxivRE = regexp.MustCompile(`^\d{4}\.\d{4,5}(v\d+)?$`)

func itemAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Add items from DOIs, arXiv ids, PDFs or a directory of PDFs — or from hand-typed fields",
		Description: "$ sci zot item add 10.1038/nature12373\n" +
			"$ sci zot item add https://doi.org/10.1038/nature12373 arXiv:1706.03762 --collection \"To read\"\n" +
			"$ sci zot item add paper.pdf                       # identifier read from the PDF, file attached\n" +
			"$ sci zot item add ~/Downloads/ --dry-run           # every *.pdf in it; see what would happen first\n" +
			"$ sci zot item add --title \"Field notes\" --author \"Smith, Alice\" --date 2020 notes.pdf\n\n" +
			"Each positional is a DOI (bare, doi:, or a doi.org URL), an arXiv id\n" +
			"(bare or arXiv:), a .pdf file, or a directory (its *.pdf files, not\n" +
			"recursive). Identifiers are resolved at doi.org, the registry every\n" +
			"registrar answers to; a PDF is read for the DOI or arXiv id on its\n" +
			"first pages (needs pdftotext from poppler) and resolved the same way,\n" +
			"then attached to the new item unless --no-attach. When a PDF carries\n" +
			"several identifiers the first in reading order is used and the rest\n" +
			"are reported as candidates (--dry-run shows them all first). Dedup\n" +
			"is by DOI, against the local mirror AND the server, so an item you\n" +
			"added moments ago is caught before desktop syncs it down; a match is\n" +
			"refused as a duplicate unless --force. An item with no DOI is never\n" +
			"matched.\n\n" +
			"With --title no identifier is looked up: the item is built from\n" +
			"--type (default journalArticle), --author \"Last, First\", --creator\n" +
			"\"type:Last, First\", --date, --url, --abstract, --publication (routed\n" +
			"to the type's venue field), --extra and --field name=value, every\n" +
			"name checked against Zotero's schema before anything is sent. The\n" +
			"only positional allowed then is one PDF to attach. Those flags are\n" +
			"refused without --title: in identifier mode the registry record is\n" +
			"the whole item.\n\n" +
			"--dry-run resolves everything (it still needs the API key and the\n" +
			"network) and reports what would be created without writing. Several\n" +
			"inputs return a per-input report; a failing input is reported and\n" +
			"the rest proceed.",
		ArgsUsage: "<doi|arxiv-id|file.pdf|dir…>",
		// Free-text values (--author "Smith, Alice") carry commas; without
		// this urfave splits them into two values.
		DisableSliceFlagSeparator: true,
		Flags:                     itemAddFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.String("title") != "" {
				return runItemAddManual(ctx, cmd)
			}
			if flag, ok := lo.Find(manualOnlyFlags, cmd.IsSet); ok {
				return cmdutil.UsageErrorf(cmd, "--%s applies only with --title (manual mode); in identifier mode the record comes from doi.org", flag)
			}
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected one or more inputs (DOI, arXiv id, file.pdf, directory), or --title for a hand-made record")
			}
			inputs, err := classifyAddInputs(cmd, cmd.Args().Slice())
			if err != nil {
				return err
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
			opts, err := readAddOptions(cmd, db)
			if err != nil {
				return err
			}
			resolver := newDOIResolver()
			if cmd.Bool("dry-run") {
				previews := lo.Map(inputs, func(in addInput, _ int) addPreview {
					return previewAddInput(ctx, client, resolver, db, in, opts)
				})
				outputScoped(ctx, cmd, addDryRunResult{DryRun: true, Count: len(previews), Items: previews})
				return nil
			}
			// Serial on purpose: Zotero rate-limits, and the client's own
			// retry layer already honours its backoff signals.
			outcomes := lo.Map(inputs, func(in addInput, _ int) addOutcome {
				return addOne(ctx, client, db, resolver, in, opts)
			})
			printCandidateNotes(ctx, outcomes)
			if len(outcomes) == 1 {
				one := outcomes[0]
				if one.Err != nil {
					return one.Err
				}
				outputScoped(ctx, cmd, one.writeResult())
				return nil
			}
			outputScoped(ctx, cmd, addBatchResult{outcomes: outcomes})
			return nil
		},
	}
}

// manualModeUsage opens the Usage of every flag that only --title reads.
const manualModeUsage = "manual mode: "

// manualOnlyFlags are the flags refused without --title: in identifier
// mode the registry record is the whole item, so a field flag there would
// be silently dropped.
var manualOnlyFlags = []string{"type", "publication", "author", "creator", "field", "date", "url", "abstract", "extra"}

// itemAddFlags assembles item add's flag list: the shared options, the
// manual-mode flags, and the dedicated field flags with their manual-mode
// prefix (--title itself is declared here, as the mode switch).
func itemAddFlags() []cli.Flag {
	shared := []cli.Flag{
		&cli.StringFlag{Name: "collection", Aliases: []string{"c"}, Usage: "file the new item(s) into this collection (key or name)", Local: true},
		&cli.StringSliceFlag{Name: "tag", Usage: "tag the new item(s); repeatable"}, // lint:no-local — slice flag, see creatorAndFieldFlags
		&cli.BoolFlag{Name: "no-attach", Usage: "for PDF inputs, create the item without uploading the file", Local: true},
		&cli.BoolFlag{Name: "dry-run", Usage: "resolve and report what would be created; write nothing (still needs the API key configured and the network, for doi.org)", Local: true},
		&cli.BoolFlag{Name: "force", Usage: "create the item even when the library already holds its DOI", Local: true},
	}
	manual := []cli.Flag{
		&cli.StringFlag{Name: "title", Usage: manualModeUsage + "the title; switches to manual mode (no identifier is looked up)", Local: true},
		&cli.StringFlag{Name: "type", Usage: manualModeUsage + "the item type (default journalArticle)", Local: true},
		&cli.StringFlag{Name: "publication", Usage: manualModeUsage + "venue, routed to publicationTitle / bookTitle / proceedingsTitle by type", Local: true},
	}
	fieldSpecs := lo.Filter(addFieldSpecs, func(s fieldFlagSpec, _ int) bool { return s.Flag != "title" })
	return slices.Concat(shared, manual, dedicatedFieldFlags(fieldSpecs, manualModeUsage), creatorAndFieldFlags(manualModeUsage))
}

// addInput is one classified positional: a PDF (Path set) or an identifier
// (DOI set — an arXiv id already mapped to its DataCite DOI).
type addInput struct {
	Arg  string
	Path string
	DOI  string
}

func (in addInput) isPDF() bool { return in.Path != "" }

// classifyAddInputs turns positionals into inputs: a directory expands to
// its *.pdf files in name order, an existing .pdf is a PDF, and anything
// else must read as exactly one DOI or arXiv id.
func classifyAddInputs(cmd *cli.Command, args []string) ([]addInput, error) {
	return lo.FlatMapErr(args, func(arg string, _ int) ([]addInput, error) {
		info, err := os.Stat(arg)
		switch {
		case err == nil && info.IsDir():
			return pdfsInDir(cmd, arg)
		case err == nil && isPDFPath(arg):
			return []addInput{{Arg: arg, Path: arg}}, nil
		case err == nil:
			return nil, cmdutil.UsageErrorf(cmd, "%s is a file but not a .pdf — item add reads identifiers out of PDFs only", arg)
		}
		doi, ok := identifierDOI(arg)
		if !ok {
			return nil, cmdutil.UsageErrorf(cmd, "%q is not a DOI, an arXiv id, a .pdf file or a directory", arg)
		}
		return []addInput{{Arg: arg, DOI: doi}}, nil
	})
}

// pdfsInDir lists a directory's *.pdf files (not recursive), sorted by
// name; a directory with none is a usage error rather than a silent no-op.
func pdfsInDir(cmd *cli.Command, dir string) ([]addInput, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	pdfs := lo.FilterMap(entries, func(e os.DirEntry, _ int) (addInput, bool) {
		path := filepath.Join(dir, e.Name())
		return addInput{Arg: path, Path: path}, !e.IsDir() && isPDFPath(e.Name())
	})
	if len(pdfs) == 0 {
		return nil, cmdutil.UsageErrorf(cmd, "%s holds no .pdf files", dir)
	}
	return pdfs, nil
}

func isPDFPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// identifierDOI reads one argument as an identifier: exactly one DOI in
// any cited form wins; else exactly one arXiv id (arXiv:-prefixed, an
// arxiv.org URL, or bare) maps to its DataCite DOI.
func identifierDOI(arg string) (string, bool) {
	refs := bib.ScanText(arg)
	dois := lo.Filter(refs, func(r bib.Ref, _ int) bool { return r.Kind == bib.KindDOI })
	arxiv := lo.Filter(refs, func(r bib.Ref, _ int) bool { return r.Kind == bib.KindArxiv })
	switch {
	case len(dois) == 1:
		return dois[0].Value, true
	case len(dois) == 0 && len(arxiv) == 1:
		return doiorg.ArxivDOI(arxiv[0].Value), true
	case len(refs) == 0 && bareArxivRE.MatchString(strings.TrimSpace(arg)):
		return doiorg.ArxivDOI(arg), true
	}
	return "", false
}

// addOptions are the flags every created item receives.
type addOptions struct {
	CollectionKey   string
	CollectionLabel string
	Tags            []string
	Attach          bool
	Force           bool
}

// readAddOptions reads the shared flags, resolving --collection against
// the mirror by key or name up front so a bad name fails before any lookup.
func readAddOptions(cmd *cli.Command, db local.Reader) (addOptions, error) {
	opts := addOptions{Tags: cmd.StringSlice("tag"), Attach: !cmd.Bool("no-attach"), Force: cmd.Bool("force")}
	if ref := cmd.String("collection"); ref != "" {
		key, coll, err := resolveCollectionRef(db, ref)
		if err != nil {
			return addOptions{}, err
		}
		opts.CollectionKey, opts.CollectionLabel = key, collectionLabel(coll, key)
	}
	return opts, nil
}

// applyTo stamps the collection and tags onto a payload about to be created.
func (o addOptions) applyTo(data map[string]any) {
	if o.CollectionKey != "" {
		data["collections"] = []string{o.CollectionKey}
	}
	if len(o.Tags) > 0 {
		data["tags"] = lo.Map(o.Tags, func(t string, _ int) map[string]any { return map[string]any{"tag": t} })
	}
}

// addResolution is what an input resolves to before anything is written.
type addResolution struct {
	Rec *csl.Record
	// Candidates are the identifiers a PDF carried after the first — a
	// reference list bleeding into the sniffed pages. Reported, never used.
	Candidates []bib.Ref
	// ExistingKey / ExistingTitle name the mirror's item carrying the DOI.
	ExistingKey   string
	ExistingTitle string
}

// resolveAddInput takes an input to its registry record: a PDF is sniffed
// for its identifier first, then both kinds go to doi.org. The dedup check
// rides along, because every caller wants it.
func resolveAddInput(ctx context.Context, client *api.Client, resolver *doiorg.Client, db local.Reader, in addInput) (addResolution, error) {
	doi := in.DOI
	var candidates []bib.Ref
	if in.isPDF() {
		refs, err := sniffPDF(ctx, in.Path)
		if err != nil {
			return addResolution{}, sniffErr(ctx, in.Path, err)
		}
		// pdfid reports identifiers in reading order, so the first is the
		// paper's own stamp in practice; the rest are distinct alternatives
		// the caller sees, never silently discarded.
		distinct := lo.UniqBy(refs, func(r bib.Ref) string { return string(r.Kind) + ":" + strings.ToLower(r.Value) })
		first := distinct[0]
		doi = first.Value
		if first.Kind == bib.KindArxiv {
			doi = doiorg.ArxivDOI(first.Value)
		}
		candidates = distinct[1:]
	}
	rec, err := resolver.Resolve(ctx, doi)
	if err != nil {
		if errors.Is(err, doiorg.ErrNotFound) {
			msg := fmt.Sprintf("doi.org has no record of %s — it may be unregistered or mistyped", doiorg.Normalize(doi))
			if in.isPDF() {
				msg += " (read from " + in.Path + ")"
			}
			return addResolution{}, cmdutil.Coded(cmdutil.CodeNotFound, "%s", msg)
		}
		return addResolution{}, err
	}
	res := addResolution{Rec: rec, Candidates: candidates}
	res.ExistingKey, res.ExistingTitle, err = findItemByDOI(ctx, client, db, rec.DOI)
	if err != nil {
		return addResolution{}, err
	}
	return res, nil
}

// findItemByDOI is the dedup gate's lookup: the local mirror first (free,
// and it knows the title), then the server for only what the mirror has
// not synced down yet — everything newer than its highest item version —
// because an item added moments ago lives only there. A miss on both
// returns "", "".
func findItemByDOI(ctx context.Context, client *api.Client, db local.Reader, doi string) (key, title string, err error) {
	hits, err := db.ItemKeysByDOI([]string{doi})
	if err != nil {
		return "", "", err
	}
	if key, ok := hits[doi]; ok {
		return key, itemTitleFromMirror(db, key), nil
	}
	synced, err := db.MaxItemVersion()
	if err != nil {
		return "", "", err
	}
	remote, err := client.FindItemsByDOISince(ctx, doi, synced)
	if err != nil {
		return "", "", writeErr(err)
	}
	if len(remote) == 0 {
		return "", "", nil
	}
	title, _ = remote[0].Data["title"].(string)
	return remote[0].Key, title, nil
}

// sniffErr maps pdfid's failures onto the coded vocabulary, each naming
// its way out: the missing tool's install, or manual mode for a PDF that
// carries no identifier.
func sniffErr(ctx context.Context, path string, err error) error {
	switch {
	case errors.Is(err, pdfid.ErrNoTool):
		return cmdutil.Coded(cmdutil.CodeNotConfigured, "reading an identifier out of a PDF needs pdftotext (poppler), which is not installed").
			WithTry("install it with `sci tools reccs --install poppler` (or `brew install poppler`) and re-run")
	case errors.Is(err, pdfid.ErrNoIdentifier):
		return cmdutil.Coded(cmdutil.CodeNotFound, "%s: no DOI or arXiv id on its first pages", path).
			WithTry(fmt.Sprintf("add it by hand and attach the file: sci zot --library %s item add --title \"…\" --author \"Last, First\" %s", scopeFromCtx(ctx), shellQuote(path)))
	}
	return err
}

// duplicateErr is the dedup gate's refusal. For a PDF the useful next step
// is attaching the file to the item that exists, which the Try names and
// the command deliberately never does on its own.
func duplicateErr(ctx context.Context, in addInput, key, title string) error {
	label := key
	if title != "" {
		label = fmt.Sprintf("%s (%q)", key, title)
	}
	scope := scopeFromCtx(ctx)
	coded := cmdutil.Coded(cmdutil.CodeDuplicate, "%s is already in this library as %s (matched by DOI, in the local mirror or on the server)", in.Arg, label)
	if in.isPDF() {
		return coded.WithTry(fmt.Sprintf("attach the file to it instead: sci zot --library %s item attach %s %s — or re-run with --force to create a second item", scope, key, shellQuote(in.Path)))
	}
	return coded.WithTry(fmt.Sprintf("sci zot --library %s item read %s shows it; re-run with --force to create a second copy anyway", scope, key))
}

// refLabel renders an identifier the way item add accepts it back: a DOI
// bare, an arXiv id with its prefix.
func refLabel(r bib.Ref) string {
	if r.Kind == bib.KindArxiv {
		return "arXiv:" + r.Value
	}
	return r.Value
}

// printCandidateNotes tells the user, on stderr, about every identifier a
// PDF carried besides the one used — and how to file the paper under one
// of the others instead. Diagnostics, so stderr under --json too.
func printCandidateNotes(ctx context.Context, outcomes []addOutcome) {
	scope := scopeFromCtx(ctx)
	for _, o := range outcomes {
		if o.Err != nil || len(o.Candidates) == 0 {
			continue
		}
		labels := lo.Map(o.Candidates, func(r bib.Ref, _ int) string { return refLabel(r) })
		_, _ = fmt.Fprintf(cmdutil.HumanErrWriter(), "  %s %s also mentions %s — if that is the paper, use `sci zot --library %s item add %s --no-attach` then `sci zot --library %s item attach <key> %s`\n",
			uikit.SymWarn, filepath.Base(o.Input.Path), strings.Join(labels, ", "), scope, labels[0], scope, shellQuote(o.Input.Path))
	}
}

// buildItemData maps a registry record onto the payload POST /items takes
// for its item type, then applies the shared flags.
func buildItemData(ctx context.Context, client *api.Client, rec *csl.Record, opts addOptions) (map[string]any, error) {
	itemType := rec.ItemType()
	fields, err := client.ItemTypeFields(ctx, itemType)
	if err != nil {
		return nil, schemaErr(itemType, err)
	}
	creatorTypes, err := client.ItemTypeCreatorTypes(ctx, itemType)
	if err != nil {
		return nil, schemaErr(itemType, err)
	}
	data := csl.ToItem(rec, fields, creatorTypes)
	opts.applyTo(data)
	return data, nil
}

// addOutcome is one input's result: what was created, whether the PDF
// made it on, and the identifiers left over.
type addOutcome struct {
	Input         addInput
	Item          *api.Object
	Title         string
	Attached      bool
	AttachmentKey string
	AttachErr     error
	Candidates    []bib.Ref
	Err           error
}

// addOne runs one input through resolve → dedup → create → attach. A
// failed attach after a successful create is recorded on the outcome, not
// returned: the item exists, and the caller can `item attach` it.
func addOne(ctx context.Context, client *api.Client, db local.Reader, resolver *doiorg.Client, in addInput, opts addOptions) addOutcome {
	out := addOutcome{Input: in}
	res, err := resolveAddInput(ctx, client, resolver, db, in)
	if err != nil {
		out.Err = err
		return out
	}
	out.Candidates = res.Candidates
	if res.ExistingKey != "" && !opts.Force {
		out.Err = duplicateErr(ctx, in, res.ExistingKey, res.ExistingTitle)
		return out
	}
	data, err := buildItemData(ctx, client, res.Rec, opts)
	if err != nil {
		out.Err = err
		return out
	}
	obj, err := client.CreateItem(ctx, data)
	if err != nil {
		out.Err = writeErr(err)
		return out
	}
	out.Item, out.Title = obj, res.Rec.Title
	if in.isPDF() && opts.Attach {
		out.attach(ctx, client)
	}
	return out
}

// writeResult renders a successful outcome as the single-input result.
func (o addOutcome) writeResult() zot.WriteResult {
	itemType, _ := o.Item.Data["itemType"].(string)
	msg := fmt.Sprintf("created %s %q (%s)", itemType, o.Title, o.Item.Key)
	if o.Attached {
		msg += " and attached " + filepath.Base(o.Input.Path)
	}
	data := map[string]any{
		"item":           o.Item,
		"attached":       o.Attached,
		"attachment_key": o.AttachmentKey,
		"candidates":     lo.Map(o.Candidates, func(r bib.Ref, _ int) string { return refLabel(r) }),
	}
	if o.AttachErr != nil {
		data["attach_error"] = o.AttachErr.Error()
		msg += fmt.Sprintf(" — attaching %s failed: %v", filepath.Base(o.Input.Path), o.AttachErr)
	}
	return zot.WriteResult{Action: "created", Kind: "item", Target: o.Item.Key, Message: msg, Data: data}
}

// addRow is one input's line in a batch report.
type addRow struct {
	Input       string `json:"input"`
	Key         string `json:"key,omitempty"`
	Title       string `json:"title,omitempty"`
	Attached    bool   `json:"attached"`
	AttachError string `json:"attach_error,omitempty"`
	Error       string `json:"error,omitempty"`
}

// addBatchResult is the several-inputs report: a BulkWriteResult whose
// Success entries are the created keys, Failed is keyed by input, and
// data.rows keeps every input in the order given.
type addBatchResult struct {
	outcomes []addOutcome
}

func (r addBatchResult) bulk() zot.BulkWriteResult {
	res := zot.BulkWriteResult{
		Action: "created",
		Kind:   "item",
		Total:  len(r.outcomes),
		Success: lo.FilterMap(r.outcomes, func(o addOutcome, _ int) (string, bool) {
			if o.Err != nil {
				return "", false
			}
			return o.Item.Key, true
		}),
		Data: map[string]any{"rows": lo.Map(r.outcomes, func(o addOutcome, _ int) addRow {
			row := addRow{Input: o.Input.Arg, Attached: o.Attached}
			if o.Err != nil {
				row.Error = o.Err.Error()
				return row
			}
			row.Key, row.Title = o.Item.Key, o.Title
			if o.AttachErr != nil {
				row.AttachError = o.AttachErr.Error()
			}
			return row
		})},
	}
	failed := lo.Filter(r.outcomes, func(o addOutcome, _ int) bool { return o.Err != nil })
	if len(failed) > 0 {
		res.Failed = lo.SliceToMap(failed, func(o addOutcome) (string, string) { return o.Input.Arg, o.Err.Error() })
	}
	return res
}

// JSON implements cmdutil.Result.
func (r addBatchResult) JSON() any { return r.bulk() }

// Human implements cmdutil.Result: one line per input, in input order.
func (r addBatchResult) Human() string {
	var b strings.Builder
	for _, o := range r.outcomes {
		switch {
		case o.Err != nil:
			fmt.Fprintf(&b, "  %s %s: %v\n", uikit.SymFail, o.Input.Arg, o.Err)
		case o.AttachErr != nil:
			fmt.Fprintf(&b, "  %s %s (%s) — attaching %s failed: %v\n", uikit.SymOK, o.Title, o.Item.Key, filepath.Base(o.Input.Path), o.AttachErr)
		case o.Attached:
			fmt.Fprintf(&b, "  %s %s (%s) +pdf\n", uikit.SymOK, o.Title, o.Item.Key)
		default:
			fmt.Fprintf(&b, "  %s %s (%s)\n", uikit.SymOK, o.Title, o.Item.Key)
		}
	}
	bulk := r.bulk()
	fmt.Fprintf(&b, "  created %d/%d item(s)\n", len(bulk.Success), bulk.Total)
	return b.String()
}

// --- dry run --------------------------------------------------------------

// addPreview is what one input WOULD become.
type addPreview struct {
	Input       string `json:"input"`
	ItemType    string `json:"item_type,omitempty"`
	Title       string `json:"title,omitempty"`
	FirstAuthor string `json:"first_author,omitempty"`
	Year        int    `json:"year,omitempty"`
	DOI         string `json:"doi,omitempty"`
	Collection  string `json:"collection,omitempty"`
	Attach      bool   `json:"attach"`
	// Identifier is the one a real run would resolve; Candidates are the
	// other identifiers a PDF carried, in reading order.
	Identifier  string   `json:"identifier,omitempty"`
	Candidates  []string `json:"candidates,omitempty"`
	ExistingKey string   `json:"existing_key,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// addDryRunResult is `item add --dry-run`'s report.
type addDryRunResult struct {
	DryRun bool         `json:"dry_run"`
	Count  int          `json:"count"`
	Items  []addPreview `json:"items"`
}

// JSON implements cmdutil.Result.
func (r addDryRunResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r addDryRunResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  dry run — nothing written; %d input(s):\n", r.Count)
	for _, p := range r.Items {
		switch {
		case p.Error != "":
			fmt.Fprintf(&b, "  %s %s: %s\n", uikit.SymFail, p.Input, p.Error)
		case p.ExistingKey != "":
			fmt.Fprintf(&b, "  %s %s: already in the library as %s (would be refused without --force)\n", uikit.SymWarn, p.Input, p.ExistingKey)
		default:
			line := fmt.Sprintf("  %s would create %s %q", uikit.SymArrow, p.ItemType, p.Title)
			if p.FirstAuthor != "" || p.Year != 0 {
				line += fmt.Sprintf(" (%s %d)", p.FirstAuthor, p.Year)
			}
			if p.DOI != "" {
				line += " doi:" + p.DOI
			}
			if p.Collection != "" {
				line += " → " + p.Collection
			}
			if p.Attach {
				line += " +pdf"
			}
			if len(p.Candidates) > 0 {
				line += fmt.Sprintf("; identifiers: %s (would use), %s", p.Identifier, strings.Join(p.Candidates, ", "))
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// previewAddInput resolves an input exactly as a real run would (doi.org
// and pdftotext included) and reports the item it would create.
func previewAddInput(ctx context.Context, client *api.Client, resolver *doiorg.Client, db local.Reader, in addInput, opts addOptions) addPreview {
	p := addPreview{Input: in.Arg, Collection: opts.CollectionKey, Attach: in.isPDF() && opts.Attach}
	res, err := resolveAddInput(ctx, client, resolver, db, in)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	rec := res.Rec
	p.ItemType, p.Title, p.DOI, p.Year = rec.ItemType(), rec.Title, rec.DOI, rec.Issued.Year
	p.Identifier = cmp.Or(in.DOI, rec.DOI)
	p.Candidates = lo.Map(res.Candidates, func(r bib.Ref, _ int) string { return refLabel(r) })
	if len(rec.Authors) > 0 {
		p.FirstAuthor = cmp.Or(rec.Authors[0].Family, rec.Authors[0].Literal)
	}
	if !opts.Force {
		p.ExistingKey = res.ExistingKey
	}
	return p
}

// --- manual mode ----------------------------------------------------------

// runItemAddManual is the --title path: no identifier, the item is built
// from flags, validated against the schema, created, and the optional PDF
// positional attached.
func runItemAddManual(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	pdfPath := ""
	switch {
	case len(args) > 1:
		return cmdutil.UsageErrorf(cmd, "with --title the only positional allowed is one PDF to attach")
	case len(args) == 1:
		if info, err := os.Stat(args[0]); err != nil || info.IsDir() || !isPDFPath(args[0]) {
			return cmdutil.UsageErrorf(cmd, "with --title the only positional allowed is one existing .pdf file to attach, got %q", args[0])
		}
		pdfPath = args[0]
	}
	edits, err := readItemEdits(cmd, addFieldSpecs)
	if err != nil {
		return err
	}
	itemType := cmp.Or(cmd.String("type"), "journalArticle")
	client, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	opts, err := readAddOptions(cmd, db)
	if err != nil {
		return err
	}
	data, err := edits.apply(ctx, client, itemType)
	if err != nil {
		return err
	}
	data["itemType"] = itemType
	opts.applyTo(data)
	title := cmd.String("title")
	attach := pdfPath != "" && opts.Attach
	if cmd.Bool("dry-run") {
		p := addPreview{Input: "--title " + title, ItemType: itemType, Title: title, Collection: opts.CollectionKey, Attach: attach}
		if creators, _ := data["creators"].([]map[string]any); len(creators) > 0 {
			last, _ := creators[0]["lastName"].(string)
			name, _ := creators[0]["name"].(string)
			p.FirstAuthor = cmp.Or(last, name)
		}
		outputScoped(ctx, cmd, addDryRunResult{DryRun: true, Count: 1, Items: []addPreview{p}})
		return nil
	}
	obj, err := client.CreateItem(ctx, data)
	if err != nil {
		return writeErr(err)
	}
	out := addOutcome{Input: addInput{Arg: "--title " + title, Path: pdfPath}, Item: obj, Title: title}
	if attach {
		out.attach(ctx, client)
	}
	outputScoped(ctx, cmd, out.writeResult())
	return nil
}

// attach uploads the outcome's PDF under its item, recording success or
// the failure — never returning it, because the item already exists.
func (o *addOutcome) attach(ctx context.Context, client *api.Client) {
	att, _, err := attachFile(ctx, client, o.Item.Key, o.Input.Path)
	if err != nil {
		o.AttachErr = err
		return
	}
	o.Attached, o.AttachmentKey = true, att.Key
}
