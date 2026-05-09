package fix

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/doi"
	"github.com/sciminds/cli/internal/zot/local"
)

// DOITarget is one planned DOI rewrite: "for item ItemKey, overwrite the
// DOI field from OldDOI to NewDOI." Emitted by PlanDOIs and consumed by
// ApplyDOIs, which performs the POST /items round-trip.
type DOITarget struct {
	ItemKey  string `json:"item_key"`
	Title    string `json:"title,omitempty"`
	OldDOI   string `json:"old_doi"`
	NewDOI   string `json:"new_doi"`
	Version  int    `json:"version"`
	ItemType string `json:"item_type"`
}

// DOIWriter is the narrow slice of the API client the DOI fix
// orchestrator needs. *api.Client satisfies this for free via
// UpdateItemsBatch.
type DOIWriter interface {
	UpdateItemsBatch(ctx context.Context, patches []api.ItemPatch) (map[string]error, error)
}

// DOIOutcome is the per-item result of an apply.
type DOIOutcome struct {
	ItemKey string `json:"item_key"`
	OldDOI  string `json:"old_doi"`
	NewDOI  string `json:"new_doi"`
	Applied bool   `json:"applied"`
	Error   string `json:"error,omitempty"`
}

// DOIResult carries both the plan (what would happen) and the outcome
// (what actually happened if we applied) so renderers can produce a
// coherent report for both --plan and --apply modes.
type DOIResult struct {
	Applied  bool         `json:"applied"`
	Targets  []DOITarget  `json:"targets"`
	Outcomes []DOIOutcome `json:"outcomes,omitempty"`
	Totals   DOITotals    `json:"totals"`
}

// DOITotals summarizes a plan or apply run. Subobject is the count of
// targets at plan time; Succeeded and Failed only tick when Applied.
type DOITotals struct {
	Subobject int `json:"subobject"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// PlanDOIs walks a slice of fully-hydrated items and returns one DOITarget
// per item whose stored DOI matches a publisher subobject pattern. The
// function is pure: no DB, no network, deterministic output order.
func PlanDOIs(items []local.Item) []DOITarget {
	targets := lo.FilterMap(items, func(it local.Item, _ int) (DOITarget, bool) {
		if it.DOI == "" || !doi.IsSubobject(it.DOI) {
			return DOITarget{}, false
		}
		return DOITarget{
			ItemKey:  it.Key,
			Title:    it.Title,
			OldDOI:   it.DOI,
			NewDOI:   doi.StripSubobject(it.DOI),
			Version:  it.Version,
			ItemType: it.Type,
		}, true
	})

	slices.SortFunc(targets, func(a, b DOITarget) int {
		return cmp.Compare(a.ItemKey, b.ItemKey)
	})
	return targets
}

// ApplyDOIs writes every target's new DOI to its item via POST /items.
// Targets are sent in batches of 50 (the Zotero API cap), calling
// opts.OnProgress between items so the CLI can drive a progress bar.
//
// Per-item errors populate DOIOutcome.Error and bump Failed without
// aborting the run. Whole-request failures (network, HTTP 5xx) surface
// as the error return so the caller can stop rather than pretend
// partial progress.
func ApplyDOIs(ctx context.Context, w DOIWriter, targets []DOITarget, opts ...ApplyOptions) (*DOIResult, error) {
	var opt ApplyOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	res := &DOIResult{
		Applied: true,
		Targets: targets,
		Totals:  DOITotals{Subobject: len(targets)},
	}
	if len(targets) == 0 {
		return res, nil
	}

	const batchSize = 50
	res.Outcomes = make([]DOIOutcome, 0, len(targets))
	done := 0

	for _, chunk := range lo.Chunk(targets, batchSize) {
		patches := lo.Map(chunk, func(tg DOITarget, _ int) api.ItemPatch {
			newDOI := tg.NewDOI
			return api.ItemPatch{
				Key:      tg.ItemKey,
				Version:  tg.Version,
				ItemType: tg.ItemType,
				Data:     client.ItemData{DOI: &newDOI},
			}
		})

		errs, err := w.UpdateItemsBatch(ctx, patches)
		if err != nil {
			return nil, fmt.Errorf("apply doi fix: %w", err)
		}

		for _, tg := range chunk {
			oc := DOIOutcome{
				ItemKey: tg.ItemKey,
				OldDOI:  tg.OldDOI,
				NewDOI:  tg.NewDOI,
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

// DryRunDOIs returns a DOIResult for --plan: the same Targets as an
// apply would touch, but with Applied=false and no outcomes. Kept
// separate from ApplyDOIs so callers can't accidentally trigger writes
// when they only meant to preview.
func DryRunDOIs(targets []DOITarget) *DOIResult {
	return &DOIResult{
		Applied: false,
		Targets: targets,
		Totals:  DOITotals{Subobject: len(targets)},
	}
}
