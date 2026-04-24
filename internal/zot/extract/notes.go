package extract

import "fmt"

// DoclingTag is applied to every child note created by the extraction
// pipeline. Searching for this tag finds every sci-managed extraction.
const DoclingTag = "docling"

// MarkdownTag is applied to the parent item whenever it has at least
// one DoclingTag-tagged child note. Lets users build a Zotero saved
// search like `attachmentFileType:is:PDF + tag:doesNotInclude:has-markdown`
// to surface items still missing an extraction.
const MarkdownTag = "has-markdown"

// Action is the planner's decision for a single extraction request.
type Action int

const (
	// ActionCreate posts a new child note — no prior docling-tagged
	// note exists for this parent item.
	ActionCreate Action = iota
	// ActionSkip is a no-op — the parent already has a docling-tagged
	// child note and --force was not set.
	ActionSkip
)

// String renders Action for JSON/Human output.
func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "create"
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
	DOI       string // parent item's DOI, if available
	Force     bool
}

// Plan is the output of PlanExtract — a decision plus enough context
// for Execute to carry it out. A dry-run returns the Plan as-is; an
// apply passes it to Execute which fills in the rendered body and
// calls the NoteWriter.
type Plan struct {
	Request PlanRequest
	Action  Action
	Reason  string
}

// PlanExtract decides whether to Create or Skip based on whether the
// parent already has a docling-tagged child note in the local DB.
//
// Decision table:
//
//	hasExisting=false               → Create
//	hasExisting=true, force=false   → Skip
//	hasExisting=true, force=true    → Create (new note alongside existing)
func PlanExtract(req PlanRequest, hasExisting bool) *Plan {
	if hasExisting && !req.Force {
		return &Plan{
			Request: req,
			Action:  ActionSkip,
			Reason:  "docling note already exists (use --force to create another)",
		}
	}
	reason := "no existing docling note"
	if req.Force && hasExisting {
		reason = "force (creating new note alongside existing)"
	}
	return &Plan{Request: req, Action: ActionCreate, Reason: reason}
}
