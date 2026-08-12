// Package backfill applies a DOI patch plan produced by `zot backfill`.
//
// The division of labour is the same one that governs OpenAlex: zot
// decides, sci writes. zot owns title_norm and the resolution tiers, so it
// is the only thing that can say WHICH DOI belongs on an item; sci holds
// the Zotero credential, so it is the only thing that can put it there.
// Neither imports the other — they meet at a file.
//
// The plan states a DOI and a provenance string. It deliberately does NOT
// state the Extra field to write, because Zotero has no field-level write:
// a PATCH carrying `extra` replaces the whole thing. Extra is therefore
// composed at write time from the server's own copy, which is what keeps a
// backfill from erasing a note someone added on another device.
package backfill

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/sciminds/sci/internal/zot/client"
)

// provenanceKey is the Extra line that records where a DOI came from.
// zot's item loader parses the same prefix back out.
const provenanceKey = "DOI-source:"

// Plan is one row of the NDJSON plan, in either of the two kinds zot
// emits: a DOI row (from `zot backfill`) or a FIELD row (from `zot
// enrich`). Exactly one of DOI and Fields is set — a row that says both is
// two plans wearing one record, and a row that says neither writes nothing.
type Plan struct {
	Library   string `json:"library"`
	ItemKey   string `json:"item_key"`
	DOI       string `json:"doi,omitempty"`
	DOISource string `json:"doi_source,omitempty"`
	// Fields is a field row's payload: Zotero field name -> the value to
	// write. It carries VALUES, not a work id to re-derive them from. sci
	// can rebuild an abstract from OpenAlex itself, and two decoders
	// reading two caches is two chances for the library to disagree with
	// the snapshot about what it holds.
	Fields map[string]string `json:"fields,omitempty"`
	// Force replaces values already on the server, and is the one way to
	// do so. It is per ROW, never a flag: correcting zot's OWN past output
	// (394 page ranges written with the wrong separator) is a different act
	// from overwriting a publisher's value, and a plan file that states
	// which of its rows are repairs can be read before it is run. A blanket
	// --force would put every future plan one typo away from flattening the
	// library.
	Force    bool   `json:"force,omitempty"`
	ItemType string `json:"item_type,omitempty"`
	WorkID   string `json:"work_id,omitempty"`
	Basis    string `json:"basis,omitempty"`
	Why      string `json:"why"`
}

// IsFieldPlan reports which kind of row this is.
func (p Plan) IsFieldPlan() bool { return len(p.Fields) > 0 }

// ByLibrary groups a plan by the library each row names.
//
// A plan spans both libraries because the corpus does, and an item key is
// only unique WITHIN one. Applying every row against a single scope makes
// the other library's keys come back "not found", which reads as a broken
// plan rather than a misrouted write — and quietly leaves half the
// backfill undone.
func ByLibrary(plans []Plan) map[string][]Plan {
	return lo.GroupBy(plans, func(p Plan) string { return p.Library })
}

// Writer is the narrow write contract — *api.Client satisfies it.
type Writer interface {
	UpdateItemsBatch(ctx context.Context, patches []api.ItemPatch) (map[string]error, error)
}

// Reader fetches the server's current copy of a set of items.
//
// It is REQUIRED, not an optimization. Extra is composed from the server's
// value, and UpdateItemsBatch's Rebuild hook fires only after a 412 — so a
// patch whose Data is left empty in the hope that Rebuild will fill it
// POSTs nothing at all, and Zotero cheerfully reports success. That bug
// wrote 709 empty patches and reported "applied 709 of 709".
type Reader interface {
	ListItems(ctx context.Context, opts api.ListItemsOptions) ([]client.Item, error)
}

// Result summarizes an application.
type Result struct {
	Applied int               `json:"applied"`
	Skipped int               `json:"skipped"`
	Failed  int               `json:"failed"`
	Errors  map[string]string `json:"errors,omitempty"`
	// FieldsWritten and FieldsSkipped count FIELDS, where the three above
	// count items. A field plan's premise is per field, so an item can be
	// applied and still have left two of its five fields alone — and a plan
	// of 4,840 fills whose result says 4,833 needs somewhere to say why.
	FieldsWritten int `json:"fields_written,omitempty"`
	FieldsSkipped int `json:"fields_skipped,omitempty"`
	// FieldsOverwritten counts values REPLACED rather than filled. Kept
	// apart from FieldsWritten because the two carry different risk: a fill
	// cannot destroy anything and a replacement can.
	FieldsOverwritten int `json:"fields_overwritten,omitempty"`
	// SkippedKeys names the items whose premise no longer held, so a
	// shrinking "applied" count is explainable rather than mysterious.
	SkippedKeys []string `json:"skipped_keys,omitempty"`
}

// ErrSuperseded means the item already carries a DOI, so the plan's
// premise — that it had none — is no longer true.
var ErrSuperseded = errors.New("item already has a DOI")

// Read loads a plan file, refusing rows that cannot be applied.
//
// Validation happens here rather than at write time on purpose: a plan
// half-applied before a bad row is noticed leaves the library in a state
// no one planned.
func Read(path string) ([]Plan, error) {
	f, err := os.Open(path) //nolint:gosec // path is the operator's own plan
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Plan
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var p Plan
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, fmt.Errorf("plan line %d: %w", line, err)
		}
		switch {
		case p.Library == "":
			return nil, fmt.Errorf("plan line %d (%s): no library — the corpus spans both, so a row cannot say where its key lives by omission", line, p.ItemKey)
		case p.ItemKey == "":
			return nil, fmt.Errorf("plan line %d: no item_key", line)
		case p.DOI != "" && len(p.Fields) > 0:
			return nil, fmt.Errorf("plan line %d (%s): carries both a doi and fields — those are two different plans with two different premises, and one row cannot state both", line, p.ItemKey)
		case p.DOI == "" && len(p.Fields) == 0:
			return nil, fmt.Errorf("plan line %d (%s): writes nothing — a patch with neither a doi nor any fields is a bug, not a no-op", line, p.ItemKey)
		case p.DOI != "" && p.DOISource == "":
			return nil, fmt.Errorf("plan line %d (%s): no doi_source — an unprovenanced DOI is indistinguishable from a publisher's", line, p.ItemKey)
		}
		if p.IsFieldPlan() {
			if err := validateFields(p, line); err != nil {
				return nil, err
			}
		}
		out = append(out, p)
	}
	return out, sc.Err()
}

// Apply writes the plan.
//
// Each batch is READ from the server first, because the payload depends on
// the server's own Extra and on whether the item has since gained a DOI.
// Rebuild is supplied too, but only covers the 412 path — it fires when a
// version conflict is detected, not on the way in.
func Apply(ctx context.Context, r Reader, w Writer, plans []Plan) (*Result, error) {
	res := &Result{}
	if len(plans) == 0 {
		return res, nil
	}

	const batchSize = 50
	for _, chunk := range lo.Chunk(plans, batchSize) {
		keys := lo.Map(chunk, func(p Plan, _ int) string { return p.ItemKey })
		items, err := r.ListItems(ctx, api.ListItemsOptions{ItemKeys: keys, Limit: len(keys)})
		if err != nil {
			return res, err
		}
		cur := lo.SliceToMap(items, func(it client.Item) (string, client.Item) {
			if it.Data.Key == nil {
				return "", it
			}
			return *it.Data.Key, it
		})

		stats := map[string]*fieldStats{}
		var patches []api.ItemPatch
		for _, p := range chunk {
			it, ok := cur[p.ItemKey]
			if !ok {
				res.Failed++
				if res.Errors == nil {
					res.Errors = map[string]string{}
				}
				res.Errors[p.ItemKey] = "not found in this library"
				continue
			}
			st := &fieldStats{}
			stats[p.ItemKey] = st
			body, buildErr := composeFor(&it, p, st)
			if buildErr != nil {
				res.Skipped++
				res.SkippedKeys = append(res.SkippedKeys, p.ItemKey)
				res.FieldsSkipped += st.skipped
				continue
			}
			var version int
			if it.Data.Version != nil {
				version = *it.Data.Version
			}
			patches = append(patches, api.ItemPatch{
				Key:      p.ItemKey,
				Version:  version,
				ItemType: string(it.Data.ItemType),
				Data:     body,
				// Re-derive on a 412 rather than restamping the version.
				// The DOI path REFUSES (its single premise is void once the
				// item has a DOI); the field path RE-DERIVES, because one
				// field losing its premise says nothing about the others.
				// Which of the two applies is a property of the plan, not a
				// choice made here — see composeFor.
				Rebuild: func(fresh *client.Item) (client.ItemData, error) {
					return composeFor(fresh, p, st)
				},
			})
		}
		if len(patches) == 0 {
			continue
		}

		results, err := w.UpdateItemsBatch(ctx, patches)
		if err != nil {
			return res, err
		}
		for key, e := range results {
			switch {
			case e == nil:
				res.Applied++
				if st := stats[key]; st != nil {
					res.FieldsWritten += st.written
					res.FieldsSkipped += st.skipped
					res.FieldsOverwritten += st.overwritten
				}
			case errors.Is(e, ErrSuperseded), errors.Is(e, ErrNothingToFill):
				res.Skipped++
				res.SkippedKeys = append(res.SkippedKeys, key)
			default:
				res.Failed++
				if res.Errors == nil {
					res.Errors = map[string]string{}
				}
				res.Errors[key] = e.Error()
			}
		}
	}
	return res, nil
}

// composeFor routes a row to the composer its kind requires. Both are used
// for the initial payload AND the 412 rebuild, so neither can drift from
// itself — which is how a rebuild ends up writing something the plan never
// intended.
func composeFor(cur *client.Item, p Plan, st *fieldStats) (client.ItemData, error) {
	if p.IsFieldPlan() {
		return composeFields(cur, p, st)
	}
	return compose(cur, p)
}

// compose builds a DOI row's patch body from the server's copy of an item.
func compose(cur *client.Item, p Plan) (client.ItemData, error) {
	if cur != nil && cur.Data.DOI != nil && strings.TrimSpace(*cur.Data.DOI) != "" {
		return client.ItemData{}, ErrSuperseded
	}
	var existing string
	if cur != nil && cur.Data.Extra != nil {
		existing = *cur.Data.Extra
	}
	doi := p.DOI
	extra := withProvenance(existing, p.DOISource)
	return client.ItemData{DOI: &doi, Extra: &extra}, nil
}

// withProvenance returns extra with exactly one DOI-source line, set to
// source.
//
// Replacing rather than appending is what makes re-running a plan safe.
// Provenance that gains a line per run stops being provenance and becomes
// a log, and the reader — zot's item loader — takes the first match.
func withProvenance(extra, source string) string {
	kept := make([]string, 0, 8)
	for _, line := range strings.Split(extra, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), strings.ToLower(provenanceKey)) {
			continue
		}
		kept = append(kept, line)
	}
	// Trim only trailing blanks: a blank line a user put between notes is
	// theirs, but one left behind by removing the old provenance is ours.
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	kept = append(kept, provenanceKey+" "+source)
	return strings.TrimLeft(strings.Join(kept, "\n"), "\n")
}

// Merge folds another Result into this one, for a plan applied across
// several libraries. Counts add; per-key maps and lists concatenate,
// because a key is unique within a library and the plan spans two.
func (r *Result) Merge(other *Result) {
	if other == nil {
		return
	}
	r.Applied += other.Applied
	r.Skipped += other.Skipped
	r.Failed += other.Failed
	r.FieldsWritten += other.FieldsWritten
	r.FieldsSkipped += other.FieldsSkipped
	r.FieldsOverwritten += other.FieldsOverwritten
	r.SkippedKeys = append(r.SkippedKeys, other.SkippedKeys...)
	for k, v := range other.Errors {
		if r.Errors == nil {
			r.Errors = map[string]string{}
		}
		r.Errors[k] = v
	}
}
