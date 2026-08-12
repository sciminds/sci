package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/extract"
	"github.com/sciminds/sci/internal/zot/notemd"
	"github.com/sciminds/sci/pkg/local"
	"github.com/urfave/cli/v3"
)

// notes-command flag destinations (package-scoped).
var (
	notesReadMD bool

	notesDeleteAll bool
	notesDeleteYes bool

	notesListLimit  int
	notesListOffset int

	notesUpdateReextract  bool
	notesUpdateHTML       bool
	notesUpdateDevice     string
	notesUpdateOCR        bool
	notesUpdateNumThreads int
	notesUpdateYes        bool
)

// extractionMoved is the shared explanation on every retired verb: one
// sentence on what changed and why, so the error teaches the model rather
// than just redirecting.
const extractionMoved = "an extraction is the paper's text, not a note — the extraction verbs " +
	"now live under `zot content`, and `zot notes` means the notes you wrote"

func notesCommand() *cli.Command {
	return &cli.Command{
		Name:    "notes",
		Aliases: []string{"note"},
		Usage:   "The notes YOU wrote (list, read)",
		Description: "Notes you authored — not docling extractions. Extractions are the\n" +
			"paper's text and live under `zot content`.\n\n" +
			"$ sci zot notes list              # your notes, attached and standalone\n" +
			"$ sci zot notes read NOTECH11     # one note's body\n" +
			"$ sci zot notes read NOTECH11 --md --json   # markdown, for piping",
		Commands: []*cli.Command{
			notesListCommand(),
			notesReadCommand(),

			// The moved verbs stay registered so they can explain
			// themselves; urfave would otherwise answer with a bare
			// "command not found".
			retiredCommand("add", "moved to `zot content extract`",
				[]string{"notes", "add"}, []string{"content", "extract"},
				extractionMoved, "--apply"),
			retiredCommand("update", "moved to `zot content refresh`",
				[]string{"notes", "update"}, []string{"content", "refresh"},
				extractionMoved),
			retiredCommand("delete", "moved to `zot content drop`",
				[]string{"notes", "delete"}, []string{"content", "drop"},
				extractionMoved),
		},
	}
}

func notesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List the notes you wrote",
		Description: "$ sci zot notes list                 # your notes (attached + standalone)\n" +
			"$ sci zot notes list --limit 0       # all of them\n" +
			"$ sci zot notes list --limit 25 --offset 50   # paginate\n\n" +
			"Docling extractions are excluded — they are the paper's text, not\n" +
			"a note. List those with `sci zot content list`.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max notes to surface (0 = unlimited)", Destination: &notesListLimit, Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Destination: &notesListOffset, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			notes, err := db.ListNotes()
			if err != nil {
				return err
			}
			page := paginate(notes, notesListOffset, notesListLimit)
			outputScoped(ctx, cmd, zot.RealNotesListResult{
				Count:  len(page),
				Total:  len(notes),
				Offset: notesListOffset,
				Notes:  page,
			})
			return nil
		},
	}
}

func contentListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List items that have a docling extraction",
		Description: "$ sci zot content list                          # first 50 extractions (default cap)\n" +
			"$ sci zot content list --limit 0                # all (warning: thousands on real libraries)\n" +
			"$ sci zot content list --limit 25 --offset 50   # paginate\n" +
			"$ sci zot content list AAAA1111                 # extractions for one item",
		ArgsUsage: "[parent-item-key]",
		Flags: []cli.Flag{
			// Default 50 because real libraries have thousands of notes
			// and an unguarded list dumps ~6000 lines into the agent's
			// context window. 0 = unlimited for power users.
			&cli.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 50, Usage: "max notes to surface (0 = unlimited)", Destination: &notesListLimit, Local: true},
			&cli.IntFlag{Name: "offset", Value: 0, Usage: "pagination offset", Destination: &notesListOffset, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if cmd.Args().Len() > 0 {
				return notesListForParent(ctx, cmd, db, cmd.Args().First())
			}
			return notesListAll(ctx, cmd, db)
		},
	}
}

// paginate slices xs[offset:offset+limit] safely. limit<=0 means
// "everything from offset". An out-of-range offset returns an empty
// slice rather than an error — the caller's footer/Total still tells
// the user how many they could have seen.
func paginate[T any](xs []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(xs) {
		return nil
	}
	end := len(xs)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return xs[offset:end]
}

func notesListForParent(ctx context.Context, cmd *cli.Command, db local.Reader, parentKey string) error {
	notes, err := db.ListDoclingNotes(parentKey)
	if err != nil {
		return err
	}
	summaries := lo.Map(notes, func(ch local.ChildItem, _ int) local.DoclingNoteSummary {
		return local.DoclingNoteSummary{
			NoteKey:   ch.Key,
			ParentKey: parentKey,
			Body:      ch.Note,
			Tags:      ch.Tags,
		}
	})
	page := paginate(summaries, notesListOffset, notesListLimit)
	outputScoped(ctx, cmd, zot.NotesListResult{
		ParentKey: parentKey,
		Count:     len(page),
		Total:     len(summaries),
		Offset:    notesListOffset,
		Notes:     page,
	})
	return nil
}

func notesListAll(ctx context.Context, cmd *cli.Command, db local.Reader) error {
	notes, err := db.ListAllDoclingNotes()
	if err != nil {
		return err
	}
	page := paginate(notes, notesListOffset, notesListLimit)
	outputScoped(ctx, cmd, zot.NotesListResult{
		Count:  len(page),
		Total:  len(notes),
		Offset: notesListOffset,
		Notes:  page,
	})
	return nil
}

func notesReadCommand() *cli.Command {
	return &cli.Command{
		Name:  "read",
		Usage: "Show the full body of a note",
		Description: "$ sci zot notes read NOTECH10\n" +
			"$ sci zot notes read NOTECH10 --md --json   # body as markdown, for piping into a model",
		ArgsUsage: "<note-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "md", Usage: "also emit the body converted to markdown (--json adds a `markdown` field)", Destination: &notesReadMD, Local: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() == 0 {
				return cmdutil.UsageErrorf(cmd, "expected a note key")
			}
			noteKey := cmd.Args().First()
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			nd, err := db.ReadNote(noteKey)
			if err != nil {
				return err
			}
			res := zot.NoteReadResult{Note: *nd}
			if notesReadMD {
				md, err := notemd.HTMLToMarkdown(nd.Body)
				if err != nil {
					return err
				}
				res.Markdown = md
			}
			outputScoped(ctx, cmd, res)
			return nil
		},
	}
}

func contentRefreshCommand() *cli.Command {
	return &cli.Command{
		Name:  "refresh",
		Usage: "Re-extract a paper and update its text in place",
		Description: "$ sci zot content refresh AAAA1111\n" +
			"$ sci zot content refresh AAAA1111 --reextract  # force re-run docling",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run", Destination: &notesUpdateReextract, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting", Destination: &notesUpdateHTML, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "auto", Destination: &notesUpdateDevice, Local: true},
			&cli.BoolFlag{Name: "ocr", Usage: "OCR scanned/bitmap content (off by default; needs a working docling OCR engine — install its deps yourself if docling errors)", Destination: &notesUpdateOCR, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = default)", Destination: &notesUpdateNumThreads, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &notesUpdateYes, Local: true},
		},
		Action: notesUpdateAction,
	}
}

func notesUpdateAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cmdutil.UsageErrorf(cmd, "expected exactly one parent item key")
	}
	parentKey := cmd.Args().First()

	if handled, err := maybeDelegateExtract(cmd); handled {
		return err
	}

	cfg, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	att, err := db.ResolvePDFAttachment(parentKey)
	if err != nil {
		return err
	}

	// Find the existing docling note to update.
	noteKeys, err := db.DoclingNoteKeys(parentKey)
	if err != nil {
		return err
	}
	if len(noteKeys) == 0 {
		return fmt.Errorf("no docling note found for %s — use `sci zot notes add` to create one", parentKey)
	}
	existingKey := noteKeys[0] // update the oldest (first-created)

	pdfPath := filepath.Join(cfg.DataDir, "storage", att.Key, att.Filename)
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("PDF attachment %s missing on disk at %s: %w", att.Key, pdfPath, err)
	}

	hash, err := extract.HashPDF(pdfPath)
	if err != nil {
		return fmt.Errorf("hash PDF: %w", err)
	}

	if done, err := cmdutil.ConfirmOrSkip(notesUpdateYes,
		fmt.Sprintf("Re-extract and update note %s for %s?", existingKey, att.Title)); done || err != nil {
		return err
	}

	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		return err
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}
	if notesUpdateReextract {
		cache.Delete(att.Key, hash)
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	ex, err := extract.NewDoclingExtractor()
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "sci-extract-*")
	if err != nil {
		return fmt.Errorf("mkdir temp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	opts := extract.ZoteroDefaults()
	if notesUpdateDevice != "" {
		opts.Device = notesUpdateDevice
	}
	opts.NumThreads = notesUpdateNumThreads
	opts.OCR = notesUpdateOCR

	// A configured extract.dir makes refresh rebuild both stores: the
	// run goes full-form over a staged KEY.pdf symlink, bypasses the
	// markdown cache (a cached body can't produce the DoclingDocument
	// JSON the layout needs), and re-finalizes the key dir below.
	var layout *extract.KeyLayout
	execPDFPath := pdfPath
	execCache := cache
	if cfg.Extract.Dir != "" {
		layout = &extract.KeyLayout{Dir: cfg.Extract.Dir}
		opts.Formats = []extract.OutputFormat{extract.FormatMarkdown, extract.FormatJSON}
		opts.ImageMode = extract.ImageReferenced
		staged, err := extract.StageKeyPDF(tmp, parentKey, pdfPath)
		if err != nil {
			return err
		}
		execPDFPath = staged
		execCache = nil
		cache.Delete(att.Key, hash) // drop any stale pre-layout entry
	}

	// Force=true so PlanExtract returns ActionCreate (we want to re-extract).
	plan := extract.PlanExtract(extract.PlanRequest{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
		PDFHash:   hash,
		DOI:       att.DOI,
		Force:     true,
	}, true) // hasExisting=true, Force=true → ActionCreate

	// Ctrl-C / SIGTERM / ssh drop must cancel ctx so the docling process
	// group dies with us instead of orphaning.
	ctx, stop := extractContext(ctx)
	defer stop()

	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:          plan,
		Extractor:     ex,
		Writer:        apiClient, // still needed for the interface, but won't be called
		PDFPath:       execPDFPath,
		OutputDir:     tmp,
		ExtractOpts:   opts,
		Cache:         execCache,
		RenderHTML:    notesUpdateHTML,
		UpdateNoteKey: existingKey,
		Updater:       apiClient,
	})
	if ctx.Err() != nil {
		return errExtractInterrupted()
	}
	if err != nil {
		return err
	}
	if layout != nil {
		secs := 0.0
		if result.Extraction != nil {
			secs = result.Extraction.Duration.Seconds()
		}
		if _, err := layout.Finalize(parentKey, tmp, pdfPath, secs); err != nil {
			return err
		}
	}

	out := zot.NoteUpdateResult{
		ParentKey: parentKey,
		PDFName:   att.Title,
		NoteKey:   result.NoteKey,
	}
	if result.Extraction != nil {
		out.ToolVersion = result.Extraction.ToolVersion
		out.Duration = result.Extraction.Duration
	}
	outputScoped(ctx, cmd, out)
	return nil
}

func contentDropCommand() *cli.Command {
	return &cli.Command{
		Name:    "drop",
		Aliases: []string{"delete", "trash"},
		Usage:   "Trash the docling extraction(s) for an item",
		Description: "$ sci zot content drop AAAA1111            # trash extractions for one item\n" +
			"$ sci zot content drop --all                # trash ALL extractions in library\n" +
			"$ sci zot content drop AAAA1111 --yes       # skip confirmation",
		ArgsUsage: "[parent-item-key]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "trash all extractions in the entire library", Destination: &notesDeleteAll, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &notesDeleteYes, Local: true},
		},
		Action: notesDeleteAction,
	}
}

func notesDeleteAction(ctx context.Context, cmd *cli.Command) error {
	if notesDeleteAll && cmd.Args().Len() > 0 {
		return cmdutil.UsageErrorf(cmd, "--all is mutually exclusive with a parent key argument")
	}
	if !notesDeleteAll && cmd.Args().Len() == 0 {
		return cmdutil.UsageErrorf(cmd, "expected a parent item key, or use --all for the entire library")
	}

	if notesDeleteAll {
		return notesDeleteAllAction(ctx, cmd)
	}
	return notesDeleteSingleAction(ctx, cmd, cmd.Args().First())
}

func notesDeleteSingleAction(ctx context.Context, cmd *cli.Command, parentKey string) error {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	noteKeys, err := db.DoclingNoteKeys(parentKey)
	if err != nil {
		return err
	}

	if len(noteKeys) == 0 {
		outputScoped(ctx, cmd, zot.NoteDeleteResult{ParentKey: parentKey})
		return nil
	}

	msg := fmt.Sprintf("Trash %d docling note(s) for %s?", len(noteKeys), parentKey)
	if done, err := cmdutil.ConfirmOrSkip(notesDeleteYes, msg); done || err != nil {
		return err
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	result := zot.NoteDeleteResult{
		ParentKey: parentKey,
		Total:     len(noteKeys),
	}
	for _, key := range noteKeys {
		if err := apiClient.TrashItem(ctx, key); err != nil {
			if result.Failed == nil {
				result.Failed = map[string]string{}
			}
			result.Failed[key] = err.Error()
			continue
		}
		result.Trashed = append(result.Trashed, key)
	}

	// All docling notes for this parent are gone — strip the
	// has-markdown marker so saved searches see the parent again.
	// Only remove if at least one trash succeeded; otherwise the
	// invariant (parent tagged ⇔ has docling note) was untouched.
	if len(result.Trashed) > 0 && len(result.Trashed) == len(noteKeys) {
		if err := apiClient.RemoveTagFromItem(ctx, parentKey, extract.MarkdownTag); err != nil {
			if result.Failed == nil {
				result.Failed = map[string]string{}
			}
			result.Failed[parentKey+":has-markdown"] = err.Error()
		} else {
			result.UntaggedParents = []string{parentKey}
		}
	}

	outputScoped(ctx, cmd, result)
	return nil
}

func notesDeleteAllAction(ctx context.Context, cmd *cli.Command) error {
	_, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	notes, err := db.ListAllDoclingNotes()
	if err != nil {
		return err
	}

	if len(notes) == 0 {
		outputScoped(ctx, cmd, zot.NoteDeleteResult{})
		return nil
	}

	msg := fmt.Sprintf("Trash %d docling note(s) across the entire library?", len(notes))
	if done, err := cmdutil.ConfirmOrSkip(notesDeleteYes, msg); done || err != nil {
		return err
	}

	apiClient, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}

	// Track per-parent trash outcomes so we only strip has-markdown
	// from parents whose entire set of docling notes was successfully
	// trashed. A parent with one trash success and one trash failure
	// still has a docling note in Zotero — keep its tag intact.
	parentTotal := map[string]int{}
	parentTrashed := map[string]int{}
	for _, n := range notes {
		parentTotal[n.ParentKey]++
	}

	result := zot.NoteDeleteResult{Total: len(notes)}
	for _, n := range notes {
		if err := apiClient.TrashItem(ctx, n.NoteKey); err != nil {
			if result.Failed == nil {
				result.Failed = map[string]string{}
			}
			result.Failed[n.NoteKey] = err.Error()
			continue
		}
		result.Trashed = append(result.Trashed, n.NoteKey)
		parentTrashed[n.ParentKey]++
	}

	// Strip has-markdown from each parent whose every docling note was
	// trashed. Sort the keys for deterministic output.
	var fullyCleared []string
	for parent, total := range parentTotal {
		if parentTrashed[parent] == total {
			fullyCleared = append(fullyCleared, parent)
		}
	}
	slices.Sort(fullyCleared)
	for _, parent := range fullyCleared {
		if err := apiClient.RemoveTagFromItem(ctx, parent, extract.MarkdownTag); err != nil {
			if result.Failed == nil {
				result.Failed = map[string]string{}
			}
			result.Failed[parent+":has-markdown"] = err.Error()
			continue
		}
		result.UntaggedParents = append(result.UntaggedParents, parent)
	}

	outputScoped(ctx, cmd, result)
	return nil
}
