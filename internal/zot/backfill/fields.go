package backfill

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/zot/client"
)

// A field plan is the second kind of row `zot` emits, from `zot enrich`.
//
// Where a DOI plan writes one identifier and its provenance, a field plan
// fills whatever an item is missing that its matched OpenAlex work already
// supplies — the abstract, volume, issue, page range and PMID the sync
// fetched and paid for. The division is the same one that governs the DOI
// path: zot owns the item->work join, so it is the only thing that can say
// whether a work's facts are this item's facts; sci holds the credential,
// so it is the only thing that can write them.
//
// **The premise is per FIELD, and that is the whole difference.** A DOI
// plan asserts one thing — this item has no DOI — and if the server
// disagrees the plan is simply void. A field plan asserts five, and four of
// them can still hold when the fifth stops. Refusing the whole patch
// because a volume appeared would throw away four correct fills; writing it
// anyway would overwrite a value someone chose. So each field is checked
// against the server's copy on its own, on the way in AND again in Rebuild.

// ErrNothingToFill means every field the plan named is already set on the
// server. It is not a failure: the plan's premise held when it was built
// and has since been satisfied by something else, which is the outcome
// enrichment exists to produce.
var ErrNothingToFill = errors.New("every planned field is already set")

// fieldAccessor reads and writes one Zotero field on an ItemData.
//
// An explicit table rather than reflection over json tags: the set of
// fields worth writing back is small, deliberate, and reviewable, and a
// plan naming anything outside it must fail LOUDLY at read time rather than
// be quietly reflected onto whatever struct member happens to match. zot
// filters its own plan against what the corpus shows an item type carries,
// which under-approximates on purpose — this table is the other half of
// that contract, and Zotero's schema is the authority behind both.
type fieldAccessor struct {
	get func(client.ItemData) string
	set func(*client.ItemData, string)
}

func strField(get func(client.ItemData) *string, set func(*client.ItemData, *string)) fieldAccessor {
	return fieldAccessor{
		get: func(d client.ItemData) string {
			if v := get(d); v != nil {
				return *v
			}
			return ""
		},
		set: func(d *client.ItemData, v string) { set(d, &v) },
	}
}

// extraProperty reaches a field the GENERATED CLIENT DOES NOT MODEL.
//
// PMID is a real Zotero field — the live library holds 100 of them as field
// rows, not as Extra lines — and it is 3,054 of the 4,840 planned fills.
// The OpenAPI spec sci generates from mentions PMID only in a comment about
// the Extra field, so `client.ItemData` has no member for it. Hand-editing
// zotero.gen.go is forbidden and regenerating cannot invent what the spec
// omits, but the generated type carries an AdditionalProperties escape
// hatch and its MarshalJSON inlines it — so the field goes on the wire
// under its own name.
//
// The alternative, folding it into Extra as a `PMID: 12345` line, would put
// a field Zotero has a column for into the free-text box where zot's loader
// reads DOI provenance, and where no consumer parses it as an identifier.
func extraProperty(name string) fieldAccessor {
	return fieldAccessor{
		get: func(d client.ItemData) string {
			v, ok := d.Get(name)
			if !ok {
				return ""
			}
			s, _ := v.(string)
			return s
		},
		set: func(d *client.ItemData, v string) { d.Set(name, v) },
	}
}

var fieldAccessors = map[string]fieldAccessor{
	"abstractNote": strField(
		func(d client.ItemData) *string { return d.AbstractNote },
		func(d *client.ItemData, v *string) { d.AbstractNote = v }),
	"url": strField(
		func(d client.ItemData) *string { return d.Url },
		func(d *client.ItemData, v *string) { d.Url = v }),
	"date": strField(
		func(d client.ItemData) *string { return d.Date },
		func(d *client.ItemData, v *string) { d.Date = v }),
	"volume": strField(
		func(d client.ItemData) *string { return d.Volume },
		func(d *client.ItemData, v *string) { d.Volume = v }),
	"issue": strField(
		func(d client.ItemData) *string { return d.Issue },
		func(d *client.ItemData, v *string) { d.Issue = v }),
	"pages": strField(
		func(d client.ItemData) *string { return d.Pages },
		func(d *client.ItemData, v *string) { d.Pages = v }),
	"publicationTitle": strField(
		func(d client.ItemData) *string { return d.PublicationTitle },
		func(d *client.ItemData, v *string) { d.PublicationTitle = v }),
	"ISSN": strField(
		func(d client.ItemData) *string { return d.ISSN },
		func(d *client.ItemData, v *string) { d.ISSN = v }),
	"PMID":  extraProperty("PMID"),
	"PMCID": extraProperty("PMCID"),
}

// writableFields names the vocabulary, for an error message that tells the
// reader what to do instead of only what went wrong.
func writableFields() string {
	return strings.Join(slices.Sorted(maps.Keys(fieldAccessors)), ", ")
}

// validateFields refuses a row naming a field sci cannot write.
//
// At READ time, before any write. A plan of 3,258 items that discovers its
// eighth field is unsupported halfway through the batch has already changed
// the library in a way nobody planned; one that is refused whole can be
// regenerated and re-run.
func validateFields(p Plan, line int) error {
	names := make([]string, 0, len(p.Fields))
	for name, value := range p.Fields {
		if _, ok := fieldAccessors[name]; !ok {
			names = append(names, name)
			continue
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("plan line %d (%s): field %q carries an empty value — "+
				"a fill that writes nothing is a bug, not a no-op", line, p.ItemKey, name)
		}
	}
	if len(names) > 0 {
		slices.Sort(names)
		return fmt.Errorf("plan line %d (%s): sci cannot write %s; writable fields are %s",
			line, p.ItemKey, strings.Join(names, ", "), writableFields())
	}
	return nil
}

// fieldStats records what one item's patch actually carried, so a plan of
// 4,840 fills and a result of 4,833 is explainable rather than mysterious.
type fieldStats struct{ written, skipped int }

// composeFields builds the patch body from the server's copy.
//
// Only fields still blank there are set; the rest are counted and dropped.
// Blank means absent OR empty string, which Zotero uses interchangeably for
// "not set" — reading ” as a value would refuse to fill exactly the items
// that need filling.
func composeFields(cur *client.Item, p Plan, st *fieldStats) (client.ItemData, error) {
	var out client.ItemData
	st.written, st.skipped = 0, 0

	// Iterated in sorted order so a patch body is byte-identical across
	// runs — a diff between two dry runs should show what changed in the
	// plan, not what Go's map iteration did that morning.
	for _, name := range slices.Sorted(maps.Keys(p.Fields)) {
		acc, ok := fieldAccessors[name]
		if !ok { // unreachable: Read validates the vocabulary
			st.skipped++
			continue
		}
		if cur != nil && strings.TrimSpace(acc.get(cur.Data)) != "" {
			st.skipped++
			continue
		}
		acc.set(&out, p.Fields[name])
		st.written++
	}
	if st.written == 0 {
		return client.ItemData{}, ErrNothingToFill
	}
	return out, nil
}
