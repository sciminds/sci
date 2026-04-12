package extract

import (
	"context"
	"fmt"
	"os"
	"time"
)

// NoteWriter is the narrow interface Execute needs from the Zotero Web
// API layer. Implemented by *api.Client via its CreateChildNote +
// UpdateChildNote methods. Tests substitute a fake so the extract
// package has no direct HTTP dependency.
type NoteWriter interface {
	CreateChildNote(ctx context.Context, parentKey, htmlBody string, tags []string) (string, error)
	UpdateChildNote(ctx context.Context, noteKey, htmlBody string) error
}

// ExecuteInput bundles everything Execute needs in one struct so
// callers don't have to thread 8 positional args. Required fields are
// noted inline; Tags and Now have defaults.
type ExecuteInput struct {
	// Plan is the output of PlanExtract.
	Plan *Plan
	// Extractor runs docling (or a fake in tests).
	Extractor Extractor
	// Writer posts / patches the resulting note.
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
	// RawMarkdown, when true, stores the original docling markdown
	// wrapped in <pre> instead of rendering it as HTML. The header and
	// sentinel are identical either way, so FindSentinel and the
	// plan/dedupe logic work unchanged.
	RawMarkdown bool
	// Cache, if non-nil, is consulted before invoking the extractor
	// and populated after a successful extraction. Keyed by
	// (Plan.Request.PDFKey, Plan.Request.PDFHash). On a hit the
	// extractor is not called at all — the resume story for bulk
	// extraction: a network failure between docling and the Zotero
	// note post never costs us a re-extract, since the next run hits
	// the cache and goes straight to the writer.
	Cache *MarkdownCache
}

// ExecuteResult describes what Execute did. For ActionSkip the
// Extraction and HTMLBody fields are zero.
type ExecuteResult struct {
	// Plan is the plan that was executed (verbatim copy of the input).
	Plan *Plan
	// NoteKey is the Zotero item key of the note that now holds the
	// extraction. On Create it's the newly assigned key; on Replace or
	// Skip it's Plan.ExistingNote.
	NoteKey string
	// HTMLBody is the rendered note body posted to Zotero. Empty on Skip.
	HTMLBody string
	// Extraction is the docling result. Nil on Skip.
	Extraction *ExtractResult
}

// defaultTags is what we apply to newly created notes when the caller
// didn't supply an explicit list. Chosen so users can `zot item
// list --tag docling` to find every sci-managed extraction.
var defaultTags = []string{"docling"}

// Execute runs the action described by in.Plan: calls the extractor
// (unless Action is Skip), renders the HTML body with
// MarkdownToNoteHTML, and posts the result via in.Writer.
//
// PATCH-in-place is used for Replace so the note key and Zotero's
// internal history stay stable — consistent with the "never
// hard-delete" rule.
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
			Plan:    in.Plan,
			NoteKey: in.Plan.ExistingNote,
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
		PDFKey:    in.Plan.Request.PDFKey,
		PDFName:   in.Plan.Request.PDFName,
		Source:    extRes.ToolVersion,
		Hash:      in.Plan.Request.PDFHash,
		Generated: now(),
	}
	var html string
	if in.RawMarkdown {
		html = MarkdownToNoteRaw(md, meta)
	} else {
		html = MarkdownToNoteHTML(md, meta)
	}

	tags := in.Tags
	if tags == nil {
		tags = defaultTags
	}

	result := &ExecuteResult{
		Plan:       in.Plan,
		HTMLBody:   html,
		Extraction: extRes,
	}

	switch in.Plan.Action {
	case ActionCreate:
		key, err := in.Writer.CreateChildNote(ctx, in.Plan.Request.ParentKey, html, tags)
		if err != nil {
			return nil, fmt.Errorf("execute: create note: %w", err)
		}
		result.NoteKey = key
	case ActionReplace:
		if in.Plan.ExistingNote == "" {
			return nil, fmt.Errorf("execute: ActionReplace requires ExistingNote")
		}
		if err := in.Writer.UpdateChildNote(ctx, in.Plan.ExistingNote, html); err != nil {
			return nil, fmt.Errorf("execute: update note: %w", err)
		}
		result.NoteKey = in.Plan.ExistingNote
	default:
		return nil, fmt.Errorf("execute: unknown action %v", in.Plan.Action)
	}

	return result, nil
}
