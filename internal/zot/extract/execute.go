package extract

import (
	"context"
	"fmt"
	"os"
	"time"
)

// NoteWriter is the narrow interface Execute needs from the Zotero Web
// API layer. Implemented by *api.Client via its CreateChildNote and
// AddTagToItem methods. Tests substitute a fake so the extract package
// has no direct HTTP dependency.
//
// AddTagToItem must be idempotent: callers invoke it on every successful
// note post and it is also driven by the backfill sweep, so re-applying
// an already-present tag must be a no-op (the *api.Client implementation
// inspects the current tag set before patching).
type NoteWriter interface {
	CreateChildNote(ctx context.Context, parentKey, body string, tags []string) (string, error)
	AddTagToItem(ctx context.Context, itemKey, tag string) error
}

// NoteUpdater is the narrow interface Execute needs when updating an
// existing note in place (PATCH). Implemented by *api.Client via its
// UpdateChildNote method. Required only when ExecuteInput.UpdateNoteKey
// is set — nil is fine for create-only callers.
type NoteUpdater interface {
	UpdateChildNote(ctx context.Context, noteKey, body string) error
}

// ExecuteInput bundles everything Execute needs in one struct so
// callers don't have to thread 8 positional args. Required fields are
// noted inline; Tags and Now have defaults.
type ExecuteInput struct {
	// Plan is the output of PlanExtract.
	Plan *Plan
	// Extractor runs docling (or a fake in tests).
	Extractor Extractor
	// Writer posts the resulting note.
	Writer NoteWriter
	// PDFPath is the on-disk location of the PDF to convert.
	PDFPath string
	// OutputDir is where docling writes its artifacts. Caller owns the
	// lifecycle — Execute does not create or delete it.
	OutputDir string
	// ExtractOpts is the docling option set. Execute fills in
	// PDFPath + OutputDir before passing it to the extractor.
	ExtractOpts ExtractOptions
	// Tags applied on CreateChildNote. Nil → default ["docling"].
	Tags []string
	// Now returns the wall time stamped into NoteMeta.Generated. Nil
	// means time.Now — tests inject a fixed clock.
	Now func() time.Time
	// RenderHTML, when true, renders the docling markdown as HTML via
	// goldmark before posting. The default (false) stores the original
	// markdown with YAML frontmatter — better for LLM consumption and
	// search. The header metadata is present either way.
	RenderHTML bool
	// Cache, if non-nil, is consulted before invoking the extractor
	// and populated after a successful extraction. Keyed by
	// (Plan.Request.PDFKey, Plan.Request.PDFHash). On a hit the
	// extractor is not called at all — the resume story for bulk
	// extraction: a network failure between docling and the Zotero
	// note post never costs us a re-extract, since the next run hits
	// the cache and goes straight to the writer.
	Cache *MarkdownCache
	// UpdateNoteKey, when non-empty, tells Execute to update an
	// existing note in place via Updater instead of creating a new
	// one via Writer. The extractor still runs (or cache is hit) to
	// produce the body — only the final write changes.
	UpdateNoteKey string
	// Updater is required when UpdateNoteKey is set. Implemented by
	// *api.Client. Nil is fine for create-only callers.
	Updater NoteUpdater
}

// ExecuteResult describes what Execute did. For ActionSkip the
// Extraction and Body fields are zero.
type ExecuteResult struct {
	// Plan is the plan that was executed (verbatim copy of the input).
	Plan *Plan
	// NoteKey is the Zotero item key of the note that now holds the
	// extraction. On Create it's the newly assigned key; on Skip it's
	// empty.
	NoteKey string
	// Body is the rendered note body posted to Zotero. Empty on Skip.
	Body string
	// Extraction is the docling result. Nil on Skip.
	Extraction *ExtractResult
}

// defaultTags is what we apply to newly created notes when the caller
// didn't supply an explicit list. Chosen so users can `zot item
// list --tag docling` to find every sci-managed extraction.
var defaultTags = []string{DoclingTag}

// Execute runs the action described by in.Plan: calls the extractor
// (unless Action is Skip), renders the note body, and posts the result
// via in.Writer.
func Execute(ctx context.Context, in ExecuteInput) (*ExecuteResult, error) {
	if in.Plan == nil {
		return nil, fmt.Errorf("execute: Plan required")
	}
	if in.Extractor == nil {
		return nil, fmt.Errorf("execute: Extractor required")
	}
	if in.Writer == nil {
		return nil, fmt.Errorf("execute: Writer required")
	}

	// Skip: short-circuit before touching the extractor.
	if in.Plan.Action == ActionSkip {
		return &ExecuteResult{
			Plan: in.Plan,
		}, nil
	}

	if in.PDFPath == "" {
		return nil, fmt.Errorf("execute: PDFPath required")
	}
	if in.OutputDir == "" {
		return nil, fmt.Errorf("execute: OutputDir required")
	}

	// Copy the extract options so we don't mutate the caller's struct.
	opts := in.ExtractOpts
	opts.PDFPath = in.PDFPath
	opts.OutputDir = in.OutputDir

	var extRes *ExtractResult
	if in.Cache != nil {
		if cachedPath, ok := in.Cache.Get(in.Plan.Request.PDFKey, in.Plan.Request.PDFHash); ok {
			extRes = &ExtractResult{
				MarkdownPath: cachedPath,
				ToolVersion:  "docling (cached)",
				FromCache:    true,
			}
		}
	}
	if extRes == nil {
		var err error
		extRes, err = in.Extractor.Extract(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("execute: extract: %w", err)
		}
		if in.Cache != nil {
			mdBytes, err := os.ReadFile(extRes.MarkdownPath)
			if err != nil {
				return nil, fmt.Errorf("execute: read markdown for cache: %w", err)
			}
			if _, err := in.Cache.Put(in.Plan.Request.PDFKey, in.Plan.Request.PDFHash, mdBytes); err != nil {
				return nil, fmt.Errorf("execute: cache put: %w", err)
			}
		}
	}

	md, err := os.ReadFile(extRes.MarkdownPath)
	if err != nil {
		return nil, fmt.Errorf("execute: read markdown: %w", err)
	}

	now := time.Now
	if in.Now != nil {
		now = in.Now
	}
	meta := NoteMeta{
		ParentKey: in.Plan.Request.ParentKey,
		PDFKey:    in.Plan.Request.PDFKey,
		PDFName:   in.Plan.Request.PDFName,
		DOI:       in.Plan.Request.DOI,
		Source:    extRes.ToolVersion,
		Hash:      in.Plan.Request.PDFHash,
		Generated: now(),
	}
	var body string
	if in.RenderHTML {
		body = MarkdownToNoteHTML(md, meta)
	} else {
		body = MarkdownToNoteRaw(md, meta)
	}

	tags := in.Tags
	if tags == nil {
		tags = defaultTags
	}

	result := &ExecuteResult{
		Plan:       in.Plan,
		Body:       body,
		Extraction: extRes,
	}

	if in.UpdateNoteKey != "" {
		if in.Updater == nil {
			return nil, fmt.Errorf("execute: Updater required when UpdateNoteKey is set")
		}
		if err := in.Updater.UpdateChildNote(ctx, in.UpdateNoteKey, body); err != nil {
			return nil, fmt.Errorf("execute: update note: %w", err)
		}
		result.NoteKey = in.UpdateNoteKey
	} else {
		key, err := in.Writer.CreateChildNote(ctx, in.Plan.Request.ParentKey, body, tags)
		if err != nil {
			return nil, fmt.Errorf("execute: create note: %w", err)
		}
		result.NoteKey = key
	}
	return result, nil
}
