// Package link turns the references inside a note into Zotero relations.
//
// A standalone note already says which papers it discusses — as
// zotero:// item links, cite-keys, DOIs, arXiv ids, wikilinks. The links
// were derivable from the note body the whole time; this package derives
// them, and `zot link suggest` writes them.
//
// The shape follows internal/zot/fix: a pure Plan* over materialized
// inputs, an Apply* taking a narrow writer interface, and ONE Result type
// carrying an Applied bool — so a dry-run and the apply that follows it are
// trivially diffable. Nothing here does I/O; the CLI reads the note,
// resolves against the library, and hands the results in.
package link

import (
	"cmp"
	"context"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot/bib"
	"github.com/sciminds/sci/pkg/local"
)

// Status is what a suggestion's fate is: something to write, something
// already written, or something we refused to guess at.
type Status string

// The statuses PlanSuggest emits.
const (
	// StatusProposed is a link that would be written.
	StatusProposed Status = "proposed"
	// StatusAlreadyLinked is a reference whose relation already exists.
	// Reported rather than dropped: LinkItems is idempotent, so re-writing
	// would be harmless — but a re-run that says "10 already-linked" is what
	// makes the command legible.
	StatusAlreadyLinked Status = "already-linked"
	// StatusUnresolved is a reference that matched no item, or more than
	// one. Surfaced, never silently dropped — the same honesty gate
	// `zot bib` applies.
	StatusUnresolved Status = "unresolved"
)

// Suggestion is one proposed (or already-present, or unresolvable) relation
// between the note and an item it references.
type Suggestion struct {
	// Key is the item to relate the note to. Empty when Status is
	// StatusUnresolved — there is no item.
	Key string `json:"key,omitempty"`
	// Title names the far end so a human can tell whether it is the paper
	// they meant.
	Title string `json:"title,omitempty"`
	// Via records how the item was referenced, in the order the forms first
	// appeared. A paper cited as both a zotero:// link and a DOI is one
	// suggestion carrying both kinds.
	Via []bib.RefKind `json:"via,omitempty"`
	// Ref is the reference text as it appeared in the note. Populated for
	// unresolved references, where the raw text is the only handle the user
	// has on what went wrong.
	Ref string `json:"ref,omitempty"`
	// Reason explains an unresolved reference ("no match", "ambiguous (N
	// candidates)"), copied from bib.
	Reason string `json:"reason,omitempty"`
	// Candidates names the competing item keys behind an ambiguity.
	Candidates []string `json:"candidates,omitempty"`
	Status     Status   `json:"status"`
}

// PlanSuggest derives the relations a note's references imply.
//
// matches and unresolved come from [bib.ResolveRefs] over the note's body;
// existing is the note's current relations. The function is pure and its
// output order is deterministic: proposed first, then already-linked, then
// unresolved, each group in the order the references appeared.
//
// Three rules do the real work:
//
//   - Existing relations are subtracted into StatusAlreadyLinked, so a
//     second run proposes nothing and says so.
//   - The note's own key is dropped — Zotero rejects a self-relation, and
//     a note that links to itself is a reference to the reader, not a link.
//   - Suggestions are deduplicated by item key with their Via merged, so
//     citing one paper three ways proposes one link.
func PlanSuggest(noteKey string, matches []bib.RefMatch, unresolved []bib.Unresolved, existing local.ItemRelationSet) []Suggestion {
	linked := lo.SliceToMap(existing.Related, func(k string) (string, bool) { return k, true })

	byKey := map[string]int{} // item key → index into out
	out := make([]Suggestion, 0, len(matches)+len(unresolved))

	for _, m := range matches {
		if m.Item.Key == noteKey {
			continue
		}
		if i, seen := byKey[m.Item.Key]; seen {
			out[i].Via = appendKind(out[i].Via, m.Ref.Kind)
			continue
		}
		status := StatusProposed
		if linked[m.Item.Key] {
			status = StatusAlreadyLinked
		}
		byKey[m.Item.Key] = len(out)
		out = append(out, Suggestion{
			Key:    m.Item.Key,
			Title:  m.Item.Title,
			Via:    []bib.RefKind{m.Ref.Kind},
			Status: status,
		})
	}

	for _, u := range unresolved {
		out = append(out, Suggestion{
			Via:        []bib.RefKind{u.Kind},
			Ref:        u.Raw,
			Reason:     u.Reason,
			Candidates: u.Candidates,
			Status:     StatusUnresolved,
		})
	}

	slices.SortStableFunc(out, func(a, b Suggestion) int {
		return cmp.Compare(statusRank(a.Status), statusRank(b.Status))
	})
	return out
}

// appendKind adds kind to via unless it is already there — the same paper
// cited twice the same way is still one way.
func appendKind(via []bib.RefKind, kind bib.RefKind) []bib.RefKind {
	if slices.Contains(via, kind) {
		return via
	}
	return append(via, kind)
}

// statusRank orders a report: what you can act on leads, what is already
// done sits in the middle, what needs judgement trails.
func statusRank(s Status) int {
	switch s {
	case StatusProposed:
		return 0
	case StatusAlreadyLinked:
		return 1
	case StatusUnresolved:
		return 2
	}
	return 3
}

// Writer is the narrow slice of the Zotero API client this package needs.
// Declared here rather than in internal/zot/api so tests can substitute a
// fake without HTTP. *api.Client satisfies it via LinkItems.
type Writer interface {
	LinkItems(ctx context.Context, a, b string) error
}

// Outcome is what actually happened for one proposed link.
type Outcome struct {
	Key    string `json:"key"`
	Linked bool   `json:"linked"`
	Error  string `json:"error,omitempty"`
}

// Totals summarizes a plan or an apply. Succeeded and Failed only move when
// Applied is true.
type Totals struct {
	Proposed      int `json:"proposed"`
	AlreadyLinked int `json:"already_linked"`
	Unresolved    int `json:"unresolved"`
	Succeeded     int `json:"succeeded,omitempty"`
	Failed        int `json:"failed,omitempty"`
}

// Result carries both the plan (what would happen) and the outcome (what
// did) so one renderer serves both modes and diffing them is trivial.
// Applied is false in dry-run mode, where Outcomes is empty.
type Result struct {
	Applied     bool         `json:"applied"`
	NoteKey     string       `json:"note_key"`
	Suggestions []Suggestion `json:"suggestions"`
	Outcomes    []Outcome    `json:"outcomes,omitempty"`
	Totals      Totals       `json:"totals"`
}

// ApplyOptions configures an apply run. All fields are optional.
type ApplyOptions struct {
	// OnProgress is called after each link is attempted. done is the
	// cumulative count, total the number of proposed links. Safe to leave nil.
	OnProgress func(done, total int)
}

// DryRun returns the Result for a preview: the same suggestions an apply
// would act on, with Applied false and no outcomes. Separate from [Apply]
// so a caller that meant to preview cannot accidentally write.
func DryRun(noteKey string, ss []Suggestion) *Result {
	return &Result{NoteKey: noteKey, Suggestions: ss, Totals: countStatuses(ss)}
}

// Apply writes every proposed link through w, relating the note to each
// item. Already-linked and unresolved suggestions are carried into the
// result untouched — the first needs no write, the second has no target.
//
// A per-item failure records its error and the run continues: one paper
// that will not link must not cost the other nine. Errors are reported in
// Outcomes rather than returned, so the caller always gets a full report.
func Apply(ctx context.Context, w Writer, noteKey string, ss []Suggestion, opts ...ApplyOptions) (*Result, error) {
	var opt ApplyOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	res := &Result{Applied: true, NoteKey: noteKey, Suggestions: ss, Totals: countStatuses(ss)}

	proposed := lo.Filter(ss, func(s Suggestion, _ int) bool { return s.Status == StatusProposed })
	if len(proposed) == 0 {
		return res, nil
	}

	res.Outcomes = make([]Outcome, 0, len(proposed))
	for i, s := range proposed {
		oc := Outcome{Key: s.Key}
		if err := w.LinkItems(ctx, noteKey, s.Key); err != nil {
			oc.Error = err.Error()
			res.Totals.Failed++
		} else {
			oc.Linked = true
			res.Totals.Succeeded++
		}
		res.Outcomes = append(res.Outcomes, oc)
		if opt.OnProgress != nil {
			opt.OnProgress(i+1, len(proposed))
		}
	}
	return res, nil
}

func countStatuses(ss []Suggestion) Totals {
	var t Totals
	for _, s := range ss {
		switch s.Status {
		case StatusProposed:
			t.Proposed++
		case StatusAlreadyLinked:
			t.AlreadyLinked++
		case StatusUnresolved:
			t.Unresolved++
		}
	}
	return t
}
