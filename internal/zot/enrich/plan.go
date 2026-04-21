package enrich

import (
	"context"
	"fmt"
	"slices"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/hygiene"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// Target is one planned enrichment: for ItemKey (at Version), apply Data to
// fill the fields named in Fills. Fields absent from Fills are deliberately
// nil in Data so the PATCH leaves them untouched — we never clobber existing
// user data.
type Target struct {
	ItemKey    string            `json:"item_key"`
	Title      string            `json:"title,omitempty"`
	OpenAlexID string            `json:"openalex_id"`
	Version    int               `json:"version"`
	ItemType   string            `json:"item_type"`
	Fills      map[string]string `json:"fills"` // field name → short display of new value
	Data       client.ItemData   `json:"-"`     // PATCH body (only Fills fields set)
}

// Skipped records an item we couldn't enrich and why.
type Skipped struct {
	ItemKey string `json:"item_key"`
	Title   string `json:"title,omitempty"`
	Reason  string `json:"reason"`
}

// Lookup is the narrow OpenAlex contract — *openalex.Client satisfies this.
// Kept as an interface so tests can stub without an HTTP server.
type Lookup interface {
	ResolveWork(ctx context.Context, identifier string) (*openalex.Work, error)
}

// Writer is the narrow write contract — *api.Client satisfies via its
// UpdateItemsBatch method. Same pattern as fix.CitekeyWriter.
type Writer interface {
	UpdateItemsBatch(ctx context.Context, patches []api.ItemPatch) (map[string]error, error)
}

// ApplyResult summarizes an Apply run.
type ApplyResult struct {
	Applied int               `json:"applied"`
	Failed  int               `json:"failed"`
	Errors  map[string]string `json:"errors,omitempty"` // item key → error string
}

// PlanFromMissing walks findings emitted by hygiene.Missing and returns one
// Target per unique item-with-DOI whose OpenAlex Work provides data for the
// specific fields flagged as missing. Items without a local DOI (nothing
// unambiguous to look up) and items whose lookup errored are recorded as
// Skipped so the caller can surface them.
//
// OpenAlex is queried once per item, regardless of how many missing-field
// findings the item has.
func PlanFromMissing(
	ctx context.Context,
	db local.Reader,
	oa Lookup,
	findings []hygiene.Finding,
) ([]Target, []Skipped, error) {
	missingByKey := groupMissingFields(findings)

	// Stable ordering so tests don't flake on map iteration.
	keys := lo.Keys(missingByKey)
	slices.Sort(keys)

	var targets []Target
	var skipped []Skipped

	for _, key := range keys {
		item, err := db.Read(key)
		if err != nil {
			skipped = append(skipped, Skipped{ItemKey: key, Reason: "local read failed: " + err.Error()})
			continue
		}
		if item.DOI == "" {
			skipped = append(skipped, Skipped{
				ItemKey: key,
				Title:   item.Title,
				Reason:  "no local DOI to look up",
			})
			continue
		}
		work, err := oa.ResolveWork(ctx, item.DOI)
		if err != nil {
			skipped = append(skipped, Skipped{
				ItemKey: key,
				Title:   item.Title,
				Reason:  "openalex lookup failed: " + err.Error(),
			})
			continue
		}
		tg, ok := buildTarget(item, work, missingByKey[key])
		if !ok {
			skipped = append(skipped, Skipped{
				ItemKey: key,
				Title:   item.Title,
				Reason:  "openalex provides nothing for the missing fields",
			})
			continue
		}
		targets = append(targets, tg)
	}
	return targets, skipped, nil
}

// groupMissingFields turns the flat findings slice into "per item key, the
// set of missing field names". Only findings from the "missing" check are
// considered — other hygiene checks use the same Finding shape for different
// purposes and must not feed the enrichment pipeline.
func groupMissingFields(findings []hygiene.Finding) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range findings {
		if f.Check != "" && f.Check != "missing" {
			continue
		}
		if f.ItemKey == "" {
			continue
		}
		if out[f.ItemKey] == nil {
			out[f.ItemKey] = map[string]bool{}
		}
		out[f.ItemKey][f.Kind] = true
	}
	return out
}

// buildTarget constructs a Target whose Data patch ONLY sets fields that are
// (a) currently missing according to the findings and (b) provided by the
// OpenAlex Work. Returns ok=false when the intersection is empty.
func buildTarget(item *local.Item, work *openalex.Work, missing map[string]bool) (Target, bool) {
	full := ToItemFields(work)
	var patch client.ItemData
	patch.ItemType = full.ItemType // PATCH requires itemType per Zotero API.
	fills := map[string]string{}

	if missing[string(hygiene.FieldTitle)] && full.Title != nil {
		patch.Title = full.Title
		fills["title"] = *full.Title
	}
	if missing[string(hygiene.FieldCreators)] && full.Creators != nil && len(*full.Creators) > 0 {
		patch.Creators = full.Creators
		fills["creators"] = fmt.Sprintf("%d creator(s)", len(*full.Creators))
	}
	if missing[string(hygiene.FieldDate)] && full.Date != nil {
		patch.Date = full.Date
		fills["date"] = *full.Date
	}
	if missing[string(hygiene.FieldAbstract)] && full.AbstractNote != nil {
		patch.AbstractNote = full.AbstractNote
		fills["abstract"] = abbrev(*full.AbstractNote, 80)
	}
	if missing[string(hygiene.FieldURL)] && full.Url != nil {
		patch.Url = full.Url
		fills["url"] = *full.Url
	}
	// DOI isn't a fillable enrich target: items without a DOI are skipped
	// upstream. "pdf" and "tags" aren't derivable from OpenAlex.

	if len(fills) == 0 {
		return Target{}, false
	}
	return Target{
		ItemKey:    item.Key,
		Title:      item.Title,
		OpenAlexID: extractOpenAlexShortID(work.ID),
		Version:    item.Version,
		ItemType:   item.Type,
		Fills:      fills,
		Data:       patch,
	}, true
}

// Apply submits every target as a single UpdateItemsBatch round.
// Per-item failures are gathered into ApplyResult.Errors; a whole-request
// failure propagates as the returned error.
func Apply(ctx context.Context, w Writer, targets []Target) (*ApplyResult, error) {
	if len(targets) == 0 {
		return &ApplyResult{}, nil
	}
	patches := lo.Map(targets, func(t Target, _ int) api.ItemPatch {
		return api.ItemPatch{
			Key:      t.ItemKey,
			Version:  t.Version,
			ItemType: t.ItemType,
			Data:     t.Data,
		}
	})
	results, err := w.UpdateItemsBatch(ctx, patches)
	if err != nil {
		return nil, err
	}
	out := &ApplyResult{Errors: map[string]string{}}
	for key, perr := range results {
		if perr != nil {
			out.Failed++
			out.Errors[key] = perr.Error()
			continue
		}
		out.Applied++
	}
	if len(out.Errors) == 0 {
		out.Errors = nil
	}
	return out, nil
}

func abbrev(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
