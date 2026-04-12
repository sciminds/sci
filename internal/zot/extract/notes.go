package extract

import (
	"context"
	"fmt"
)

// ChildNote is the minimum shape PlanExtract needs from a child note.
// The api layer's GetItemChildren wrapper filters to itemType==note and
// projects to this struct so this package has no client/ coupling.
type ChildNote struct {
	Key  string // Zotero item key of the note itself
	Body string // HTML body (ItemData.Note contents)
}

// ChildLister fetches a parent item's note children. Implemented by
// *api.Client; tests use a fake.
type ChildLister interface {
	ListNoteChildren(ctx context.Context, parentKey string) ([]ChildNote, error)
}

// Action is the planner's decision for a single extraction request.
type Action int

const (
	// ActionCreate posts a new child note — no prior sci-extract note
	// exists for this (parentKey, pdfKey) pair.
	ActionCreate Action = iota
	// ActionReplace PATCHes an existing sci-extract note in place
	// (same note key, new body). Used when the PDF hash has drifted
	// or --force is set and a prior note exists. PATCH-in-place avoids
	// any destructive delete — consistent with the "never hard-delete"
	// rule; the user-visible history in Zotero stays intact.
	ActionReplace
	// ActionSkip is a no-op — the existing note's embedded hash matches
	// the current PDF hash and --force was not set.
	ActionSkip
)

// String renders Action for JSON/Human output.
func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
	case ActionReplace:
		return "replace"
	case ActionSkip:
		return "skip"
	}
	return fmt.Sprintf("Action(%d)", int(a))
}

// PlanRequest is the input to PlanExtract: fully-resolved inputs from
// the CLI layer (parent key + attachment metadata + PDF hash). Hashing
// lives in the caller so Plan stays pure and testable.
type PlanRequest struct {
	ParentKey string
	PDFKey    string
	PDFName   string
	PDFHash   string
	Force     bool
}

// Plan is the output of PlanExtract — a decision plus enough context
// for Execute to carry it out. A dry-run returns the Plan as-is; an
// apply passes it to Execute which fills in the rendered HTML and
// calls the NoteWriter.
type Plan struct {
	Request      PlanRequest
	Action       Action
	Reason       string
	ExistingNote string // set when Action is Replace or Skip
}

// PlanExtract inspects the parent's existing note children for an
// sci-extract sentinel matching req.PDFKey and decides whether to
// Create, Replace, or Skip.
//
// Decision table (req.Force = false):
//
//	no sentinel for req.PDFKey        → Create
//	sentinel hash == req.PDFHash      → Skip   (up-to-date)
//	sentinel hash != req.PDFHash      → Replace (drift)
//
// With req.Force = true: Skip is never returned. If an existing note
// matches, we Replace it (preserving the note key); otherwise Create.
func PlanExtract(ctx context.Context, lister ChildLister, req PlanRequest) (*Plan, error) {
	children, err := lister.ListNoteChildren(ctx, req.ParentKey)
	if err != nil {
		return nil, fmt.Errorf("list note children of %s: %w", req.ParentKey, err)
	}

	for _, ch := range children {
		key, hash, ok := FindSentinel(ch.Body)
		if !ok || key != req.PDFKey {
			continue
		}
		// Found our prior extraction.
		if req.Force {
			return &Plan{
				Request:      req,
				Action:       ActionReplace,
				Reason:       "force re-extract",
				ExistingNote: ch.Key,
			}, nil
		}
		if hash == req.PDFHash {
			return &Plan{
				Request:      req,
				Action:       ActionSkip,
				Reason:       "up-to-date (pdf hash unchanged)",
				ExistingNote: ch.Key,
			}, nil
		}
		return &Plan{
			Request:      req,
			Action:       ActionReplace,
			Reason:       fmt.Sprintf("pdf hash changed (%s → %s)", hash, req.PDFHash),
			ExistingNote: ch.Key,
		}, nil
	}

	reason := "no existing sci-extract note"
	if req.Force {
		reason = "force (no existing note to replace)"
	}
	return &Plan{Request: req, Action: ActionCreate, Reason: reason}, nil
}
