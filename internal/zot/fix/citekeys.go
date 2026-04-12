// Package fix owns every write-side hygiene repair for the zot tools.
// Lives in a sub-package so it can import both `internal/zot/api` (the
// Web API client) and `internal/zot/citekey` (synth + validate) without
// tripping the existing `api → internal/zot` cycle — the parent zot
// package owns Config, which api already depends on.
//
// Each fix exposes the same shape: a pure Plan* function that returns
// []Target for dry-run, an Apply* function that takes a writer
// interface and returns a *Result with per-item outcomes, and a
// narrow Writer interface so tests can bypass HTTP.
package fix

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/citekey"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/local"
)

// CitekeyKind is a bitmask selecting which categories of citekey problem a
// fix should address. The default mask (CitekeyAll) covers every category
// the read-only check surfaces; narrower masks let callers carve out
// safer subsets (e.g. invalid+collision only on a BBT-managed library
// where non-canonical is 100% of rows by design).
type CitekeyKind int

// Cite-key issue categories (bit flags for --kind filtering).
const (
	CitekeyInvalid CitekeyKind = 1 << iota
	CitekeyCollision
	CitekeyNonCanonical
	CitekeyUnstored
)

// CitekeyAll is every category the fix supports. Use this as the default
// when --kind is empty.
const CitekeyAll = CitekeyInvalid | CitekeyCollision | CitekeyNonCanonical | CitekeyUnstored

// CitekeyKindName is the stable string label for a single CitekeyKind bit,
// matching the Finding.Kind strings emitted by hygiene.Citekeys so
// filters and findings share a vocabulary. Returns "" for CitekeyAll or
// combinations (caller should iterate bits).
func CitekeyKindName(k CitekeyKind) string {
	switch k {
	case CitekeyInvalid:
		return "invalid"
	case CitekeyCollision:
		return "collision"
	case CitekeyNonCanonical:
		return "non-canonical"
	case CitekeyUnstored:
		return "unstored"
	}
	return ""
}

// ParseCitekeyKind maps a user-facing string to a CitekeyKind bit. Case-
// insensitive; accepts both "non-canonical" and "noncanonical".
func ParseCitekeyKind(s string) (CitekeyKind, bool) {
	switch s {
	case "invalid":
		return CitekeyInvalid, true
	case "collision":
		return CitekeyCollision, true
	case "non-canonical", "noncanonical":
		return CitekeyNonCanonical, true
	case "unstored":
		return CitekeyUnstored, true
	}
	return 0, false
}

// CitekeyTarget is one planned rewrite: "for item ItemKey, overwrite the
// citationKey field from OldKey to NewKey because of Reason." Emitted
// by PlanCitekeys and consumed by ApplyCitekeys, which actually
// does the POST /items round-trip.
type CitekeyTarget struct {
	ItemKey  string `json:"item_key"`
	Title    string `json:"title,omitempty"`
	OldKey   string `json:"old_key,omitempty"` // "" for unstored
	NewKey   string `json:"new_key"`
	Reason   string `json:"reason"`    // invalid | collision | non-canonical | unstored
	Version  int    `json:"version"`   // from local DB — lets UpdateItemsBatch skip per-item GETs
	ItemType string `json:"item_type"` // from local DB — ditto
}

// CitekeyOptions narrows a plan. Kinds filters which buckets contribute
// targets (0 means CitekeyAll). ItemKeys, when non-empty, is an allow-list
// of Zotero item keys — useful for single-item smoke tests via
// `--item ABCD1234`.
type CitekeyOptions struct {
	Kinds    CitekeyKind
	ItemKeys []string
}

// PlanCitekeys walks a slice of fully-hydrated items and returns one
// CitekeyTarget per item that needs a rewrite under opts. The function is
// pure: no DB, no network, deterministic output order.
//
// Classification mirrors hygiene.CitekeysFromRows with two differences.
// First, each item receives exactly one Reason even though multiple
// findings can apply (priority: invalid > collision > non-canonical >
// unstored) — the fix only needs one write per item. Second, items with
// no stored cite-key at all are treated as targets when CitekeyUnstored is
// set; the read-only check skips them.
func PlanCitekeys(items []local.Item, opts CitekeyOptions) []CitekeyTarget {
	kinds := opts.Kinds
	if kinds == 0 {
		kinds = CitekeyAll
	}
	allow := itemKeyAllowSet(opts.ItemKeys)

	// First pass: compute resolved stored key + bucket per item, and
	// build a reverse index for collision detection. We keep the full
	// item only for the targets that actually survive the filter; no
	// reason to duplicate metadata for rows we'll discard.
	type slot struct {
		item     *local.Item
		stored   string // resolved stored key ("" for unstored)
		invalid  bool
		canonAsc bool // passes v2 spec
	}
	slots := make([]slot, 0, len(items))
	keyIndex := map[string]int{} // stored key → count

	for i := range items {
		it := &items[i]
		if allow != nil && !allow[it.Key] {
			continue
		}
		stored := resolveStoredCiteKey(it)
		s := slot{item: it, stored: stored}
		if stored != "" {
			switch st, _ := citekey.Validate(stored); st {
			case citekey.Invalid:
				s.invalid = true
			case citekey.Valid:
				s.canonAsc = true
			}
			keyIndex[stored]++
		}
		slots = append(slots, s)
	}

	// Second pass: classify each slot into a single bucket using the
	// priority invalid > collision > non-canonical > unstored, then
	// drop anything whose bucket isn't enabled in `kinds` or whose
	// stored key is already canonical and uncontested.
	targets := make([]CitekeyTarget, 0, len(slots))
	for _, s := range slots {
		var reason string
		switch {
		case s.stored == "":
			reason = "unstored"
		case s.invalid:
			reason = "invalid"
		case keyIndex[s.stored] > 1:
			reason = "collision"
		case !s.canonAsc:
			reason = "non-canonical"
		default:
			continue // already canonical, no collision, nothing to do
		}
		bit, _ := ParseCitekeyKind(reason)
		if kinds&bit == 0 {
			continue
		}
		targets = append(targets, CitekeyTarget{
			ItemKey:  s.item.Key,
			Title:    s.item.Title,
			OldKey:   s.stored,
			NewKey:   citekey.Synthesize(s.item),
			Reason:   reason,
			Version:  s.item.Version,
			ItemType: s.item.Type,
		})
	}

	// Stable ordering for deterministic dry-run diffs and test goldens.
	slices.SortFunc(targets, func(a, b CitekeyTarget) int {
		if c := cmp.Compare(fixReasonRank(a.Reason), fixReasonRank(b.Reason)); c != 0 {
			return c
		}
		return cmp.Compare(a.ItemKey, b.ItemKey)
	})
	return targets
}

// resolveStoredCiteKey returns the stored cite-key for an item in the
// same resolution order as citekey.Resolve, but without ever falling
// through to synthesis. An empty return means "no stored cite-key" —
// i.e. an unstored item from the fix planner's point of view.
func resolveStoredCiteKey(it *local.Item) string {
	if k := it.Fields["citationKey"]; k != "" {
		return k
	}
	return citekey.FromExtra(it.Fields["extra"])
}

// fixReasonRank orders targets for display: errors lead, then warnings,
// then info-ish unstored. Matches doctor's severity ranking.
func fixReasonRank(reason string) int {
	switch reason {
	case "invalid":
		return 0
	case "collision":
		return 1
	case "non-canonical":
		return 2
	case "unstored":
		return 3
	}
	return 4
}

// CitekeyWriter is the narrow slice of the API client the citekey fix
// orchestrator needs. Defined here (not in `internal/zot/api`) so tests
// can pass a hand-rolled fake without spinning up an HTTP server when
// the goal is to exercise the orchestrator's logic rather than the
// generated HTTP plumbing. `*api.Client` satisfies this interface for
// free via UpdateItemsBatch.
type CitekeyWriter interface {
	UpdateItemsBatch(ctx context.Context, patches []api.ItemPatch) (map[string]error, error)
}

// CitekeyOutcome is the per-item result of an apply. Written into CitekeyResult
// so callers can tell "planner wanted to touch 5055 items and every one
// landed" from "planner wanted 5055 but 3 raised a 412 after retry".
type CitekeyOutcome struct {
	ItemKey string `json:"item_key"`
	OldKey  string `json:"old_key,omitempty"`
	NewKey  string `json:"new_key"`
	Reason  string `json:"reason"`
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}

// CitekeyResult carries both the plan (what would happen) and the outcome
// (what actually happened if we applied) so Human() and JSON() can
// render a single coherent report for both --plan and --apply modes.
// Applied is false in dry-run mode; Outcomes is empty then.
type CitekeyResult struct {
	Applied  bool             `json:"applied"`
	Targets  []CitekeyTarget  `json:"targets"`
	Outcomes []CitekeyOutcome `json:"outcomes,omitempty"`
	// Totals rolls up counts by bucket for the summary line.
	Totals CitekeyTotals `json:"totals"`
}

// CitekeyTotals summarizes a plan or apply run. PerReason counts how many
// targets fell into each bucket (invalid/collision/non-canonical/
// unstored); Succeeded and Failed only tick when Applied is true.
type CitekeyTotals struct {
	PerReason map[string]int `json:"per_reason"`
	Succeeded int            `json:"succeeded"`
	Failed    int            `json:"failed"`
}

// ApplyOptions configures the apply run. All fields are optional.
type ApplyOptions struct {
	// OnProgress is called after each item's outcome is recorded. done is
	// the cumulative count of items processed so far; total is len(targets).
	// Safe to leave nil.
	OnProgress func(done, total int)
}

// ApplyCitekeys writes every target's new cite-key to its item via
// POST /items. Targets are sent in batches of 50 (the Zotero API cap),
// calling opts.OnProgress between batches so the CLI can drive a
// progress bar. Returns one CitekeyOutcome per target; a per-item error
// populates Error, leaves Applied=false, and bumps Failed. Whole-request
// failures (network, HTTP 5xx) surface as the error return — we stop
// the run rather than pretend partial progress.
//
// After the API round-trip completes we return from the zot side
// without waiting for the local Zotero desktop's sync to catch up; the
// CLAUDE.md "reads local, writes cloud" split expects the next local
// read to pick up fresh data on its own.
func ApplyCitekeys(ctx context.Context, w CitekeyWriter, targets []CitekeyTarget, opts ...ApplyOptions) (*CitekeyResult, error) {
	var opt ApplyOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	res := &CitekeyResult{
		Applied: true,
		Targets: targets,
		Totals:  CitekeyTotals{PerReason: countByReason(targets)},
	}
	if len(targets) == 0 {
		return res, nil
	}

	// Send in chunks of 50 (the Zotero API cap), reporting progress
	// between chunks so the CLI can animate a progress bar.
	const batchSize = 50
	res.Outcomes = make([]CitekeyOutcome, 0, len(targets))
	done := 0

	for _, chunk := range lo.Chunk(targets, batchSize) {
		patches := lo.Map(chunk, func(tg CitekeyTarget, _ int) api.ItemPatch {
			newKey := tg.NewKey
			return api.ItemPatch{
				Key:      tg.ItemKey,
				Version:  tg.Version,
				ItemType: tg.ItemType,
				Data:     client.ItemData{CitationKey: &newKey},
			}
		})

		errs, err := w.UpdateItemsBatch(ctx, patches)
		if err != nil {
			return nil, fmt.Errorf("apply citekey fix: %w", err)
		}

		for _, tg := range chunk {
			oc := CitekeyOutcome{
				ItemKey: tg.ItemKey,
				OldKey:  tg.OldKey,
				NewKey:  tg.NewKey,
				Reason:  tg.Reason,
			}
			if perErr, ok := errs[tg.ItemKey]; ok && perErr != nil {
				oc.Error = perErr.Error()
				res.Totals.Failed++
			} else {
				oc.Applied = true
				res.Totals.Succeeded++
			}
			res.Outcomes = append(res.Outcomes, oc)
			done++
			if opt.OnProgress != nil {
				opt.OnProgress(done, len(targets))
			}
		}
	}
	return res, nil
}

// DryRunCitekeys returns a CitekeyResult for --plan: the same Targets as
// an apply would touch, but with Applied=false and no outcomes. Kept
// separate from ApplyCitekeys so callers can't accidentally trigger
// writes when they only meant to preview.
func DryRunCitekeys(targets []CitekeyTarget) *CitekeyResult {
	return &CitekeyResult{
		Applied: false,
		Targets: targets,
		Totals:  CitekeyTotals{PerReason: countByReason(targets)},
	}
}

// countByReason aggregates per-reason counts for the CitekeyTotals.
func countByReason(targets []CitekeyTarget) map[string]int {
	out := map[string]int{}
	for _, tg := range targets {
		out[tg.Reason]++
	}
	return out
}

// itemKeyAllowSet builds a set from a --item allow list. A nil return
// means "no filter" (all items eligible); an empty map would mean "no
// items eligible", which is never what the caller intends.
func itemKeyAllowSet(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k != "" {
			out[k] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
