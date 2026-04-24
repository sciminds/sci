package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/extract"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// notes-command flag destinations (package-scoped).
var (
	notesDeleteAll bool
	notesDeleteYes bool

	notesAddForce      bool
	notesAddReextract  bool
	notesAddHTML       bool
	notesAddDevice     string
	notesAddNumThreads int
	notesAddYes        bool

	notesUpdateReextract  bool
	notesUpdateHTML       bool
	notesUpdateDevice     string
	notesUpdateNumThreads int
	notesUpdateYes        bool
)

func notesCommand() *cli.Command {
	return &cli.Command{
		Name:    "notes",
		Aliases: []string{"note"},
		Usage:   "Manage docling extraction notes (list, read, add, update, delete)",
		Description: "$ sci zot notes list\n" +
			"$ sci zot notes list AAAA1111\n" +
			"$ sci zot notes read NOTECH10\n" +
			"$ sci zot notes add AAAA1111\n" +
			"$ sci zot notes update AAAA1111\n" +
			"$ sci zot notes delete AAAA1111\n" +
			"$ sci zot notes delete --all",
		Commands: []*cli.Command{
			notesListCommand(),
			notesReadCommand(),
			notesAddCommand(),
			notesUpdateCommand(),
			notesDeleteCommand(),
		},
	}
}

func notesListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List docling extraction notes",
		Description: "$ sci zot notes list                   # all items with docling notes\n" +
			"$ sci zot notes list AAAA1111           # docling notes for one item",
		ArgsUsage: "[parent-item-key]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()

			if cmd.Args().Len() > 0 {
				return notesListForParent(cmd, db, cmd.Args().First())
			}
			return notesListAll(cmd, db)
		},
	}
}

func notesListForParent(cmd *cli.Command, db local.Reader, parentKey string) error {
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
	cmdutil.Output(cmd, zot.NotesListResult{
		ParentKey: parentKey,
		Count:     len(summaries),
		Notes:     summaries,
	})
	return nil
}

func notesListAll(cmd *cli.Command, db local.Reader) error {
	notes, err := db.ListAllDoclingNotes()
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, zot.NotesListResult{
		Count: len(notes),
		Notes: notes,
	})
	return nil
}

func notesReadCommand() *cli.Command {
	return &cli.Command{
		Name:        "read",
		Usage:       "Show the full body of a note",
		Description: "$ sci zot notes read NOTECH10",
		ArgsUsage:   "<note-key>",
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
			cmdutil.Output(cmd, zot.NoteReadResult{Note: *nd})
			return nil
		},
	}
}

func notesAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Extract a PDF and create a docling note",
		Description: "$ sci zot notes add AAAA1111               # extract + create note\n" +
			"$ sci zot notes add AAAA1111 --force        # even if docling note exists\n" +
			"$ sci zot notes add AAAA1111 --html          # rendered HTML instead of raw markdown",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "create a new note even if a docling note already exists", Destination: &notesAddForce, Local: true},
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run", Destination: &notesAddReextract, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting", Destination: &notesAddHTML, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "mps", Destination: &notesAddDevice, Local: true},
			&cli.IntFlag{Name: "num-threads", Usage: "docling CPU threads (0 = default)", Destination: &notesAddNumThreads, Local: true},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "skip confirmation", Destination: &notesAddYes, Local: true},
		},
		Action: notesAddAction,
	}
}

func notesAddAction(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cmdutil.UsageErrorf(cmd, "expected exactly one parent item key")
	}
	parentKey := cmd.Args().First()

	cfg, db, err := openLocalDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	att, err := db.ResolvePDFAttachment(parentKey)
	if err != nil {
		return err
	}

	pdfPath := filepath.Join(cfg.DataDir, "storage", att.Key, att.Filename)
	if _, err := os.Stat(pdfPath); err != nil {
		return fmt.Errorf("PDF attachment %s missing on disk at %s: %w", att.Key, pdfPath, err)
	}

	hash, err := extract.HashPDF(pdfPath)
	if err != nil {
		return fmt.Errorf("hash PDF: %w", err)
	}

	hasExisting, err := db.ParentsWithDoclingNotes()
	if err != nil {
		return err
	}

	plan := extract.PlanExtract(extract.PlanRequest{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
		PDFHash:   hash,
		DOI:       att.DOI,
		Force:     notesAddForce,
	}, hasExisting[parentKey])

	if plan.Action == extract.ActionSkip {
		cmdutil.Output(cmd, zot.NoteAddResult{
			ParentKey: parentKey,
			PDFName:   att.Title,
			Action:    zot.ActionLabel(plan.Action),
		})
		return nil
	}

	verb := zot.ActionLabel(plan.Action)
	if done, err := cmdutil.ConfirmOrSkip(notesAddYes,
		fmt.Sprintf("%s note for %s (%s)?", verb, att.Title, plan.Reason)); done || err != nil {
		return err
	}

	// Set up cache for crash-resume.
	cacheDir, err := extract.DefaultCacheDir()
	if err != nil {
		return err
	}
	cache := &extract.MarkdownCache{Dir: cacheDir}
	if notesAddReextract {
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
	if notesAddDevice != "" {
		opts.Device = notesAddDevice
	}
	opts.NumThreads = notesAddNumThreads

	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:        plan,
		Extractor:   ex,
		Writer:      apiClient,
		PDFPath:     pdfPath,
		OutputDir:   tmp,
		ExtractOpts: opts,
		Cache:       cache,
		RenderHTML:  notesAddHTML,
	})
	if err != nil {
		return err
	}

	out := zot.NoteAddResult{
		ParentKey: parentKey,
		PDFName:   att.Title,
		NoteKey:   result.NoteKey,
		Action:    zot.ActionLabel(plan.Action),
	}
	if result.Extraction != nil {
		out.ToolVersion = result.Extraction.ToolVersion
		out.Duration = result.Extraction.Duration
	}
	cmdutil.Output(cmd, out)
	return nil
}

func notesUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Re-extract and update an existing docling note in place",
		Description: "$ sci zot notes update AAAA1111\n" +
			"$ sci zot notes update AAAA1111 --reextract  # force re-run docling",
		ArgsUsage: "<parent-item-key>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "reextract", Usage: "discard cached docling output and re-run", Destination: &notesUpdateReextract, Local: true},
			&cli.BoolFlag{Name: "html", Usage: "render markdown as HTML before posting", Destination: &notesUpdateHTML, Local: true},
			&cli.StringFlag{Name: "device", Usage: "docling accelerator (auto|cpu|mps|cuda)", Value: "mps", Destination: &notesUpdateDevice, Local: true},
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

	// Force=true so PlanExtract returns ActionCreate (we want to re-extract).
	plan := extract.PlanExtract(extract.PlanRequest{
		ParentKey: parentKey,
		PDFKey:    att.Key,
		PDFName:   att.Title,
		PDFHash:   hash,
		DOI:       att.DOI,
		Force:     true,
	}, true) // hasExisting=true, Force=true → ActionCreate

	result, err := extract.Execute(ctx, extract.ExecuteInput{
		Plan:          plan,
		Extractor:     ex,
		Writer:        apiClient, // still needed for the interface, but won't be called
		PDFPath:       pdfPath,
		OutputDir:     tmp,
		ExtractOpts:   opts,
		Cache:         cache,
		RenderHTML:    notesUpdateHTML,
		UpdateNoteKey: existingKey,
		Updater:       apiClient,
	})
	if err != nil {
		return err
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
	cmdutil.Output(cmd, out)
	return nil
}

func notesDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:    "delete",
		Aliases: []string{"trash"},
		Usage:   "Trash docling extraction notes",
		Description: "$ sci zot notes delete AAAA1111            # trash docling notes for one item\n" +
			"$ sci zot notes delete --all                # trash ALL docling notes in library\n" +
			"$ sci zot notes delete AAAA1111 --yes       # skip confirmation",
		ArgsUsage: "[parent-item-key]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "all", Usage: "trash all docling notes in the entire library", Destination: &notesDeleteAll, Local: true},
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
		cmdutil.Output(cmd, zot.NoteDeleteResult{ParentKey: parentKey})
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

	cmdutil.Output(cmd, result)
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
		cmdutil.Output(cmd, zot.NoteDeleteResult{})
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

	cmdutil.Output(cmd, result)
	return nil
}
