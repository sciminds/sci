package api

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/zot/client"
	"github.com/sciminds/sci/pkg/local"
)

// ItemFromClient converts a Zotero Web API item into the same shape used by
// local.Reader, so CLI callers (hydrated writes, --remote reads) see one
// uniform item type regardless of whether the data came from local sqlite
// or the API. Attachments are left empty — they live on child items and
// require a separate /items/{key}/children call.
func ItemFromClient(it *client.Item) local.Item {
	if it == nil {
		return local.Item{}
	}
	d := it.Data
	out := local.Item{
		Key:     it.Key,
		Type:    string(d.ItemType),
		Version: it.Version,
	}
	if d.Title != nil {
		out.Title = *d.Title
	}
	if d.Date != nil {
		out.Date = *d.Date
		out.Year = local.ParseYear(out.Date)
	}
	if d.DOI != nil {
		out.DOI = *d.DOI
	}
	if d.Url != nil {
		out.URL = *d.Url
	}
	if d.AbstractNote != nil {
		out.Abstract = *d.AbstractNote
	}
	if d.PublicationTitle != nil {
		out.Publication = *d.PublicationTitle
	}
	if d.Extra != nil {
		out.Extra = *d.Extra
	}
	out.Fields = fieldBag(d)
	if d.Creators != nil {
		out.Creators = lo.Map(*d.Creators, creatorFromClient)
	}
	if d.Tags != nil {
		out.Tags = lo.Map(*d.Tags, func(t client.Tag, _ int) string { return t.Tag })
	}
	if d.Collections != nil {
		out.Collections = *d.Collections
	}
	if d.DateAdded != nil {
		out.DateAdded = d.DateAdded.UTC().Format("2006-01-02T15:04:05Z")
	}
	if d.DateModified != nil {
		out.DateModified = d.DateModified.UTC().Format("2006-01-02T15:04:05Z")
	}
	if it.Meta != nil && it.Meta.NumChildren != nil {
		out.NumChildren = *it.Meta.NumChildren
	}
	out.Relations = relationSetFromClient(d.Relations)
	return out
}

// structuralFields are the keys fieldBag refuses. Each is already typed on
// [local.Item], and the local reader's Fields comes from Zotero's itemData
// table, which holds none of them — so letting them through would make the
// two planes disagree in the one place this projection exists to make them
// agree.
var structuralFields = map[string]bool{
	"key": true, "version": true, "itemType": true, "creators": true,
	"tags": true, "collections": true, "relations": true,
	"dateAdded": true, "dateModified": true,
}

// fieldBag projects every bibliographic field the server sent, under
// Zotero's own field names, matching what local.hydrateFields reads out of
// itemData. Returns nil when the item carries none, so the JSON shape stays
// minimal.
//
// It round-trips through JSON rather than listing ItemData's 76 typed
// fields, and that is the point rather than a shortcut. This converter used
// to seed exactly two entries, `extra` and `citationKey`, so `--remote`
// could verify those and nothing else — while sci's own contract makes a
// remote read the ground truth for confirming a write, because the local
// mirror cannot tell a field this CLI just wrote from one that was never
// there. A hand-maintained list going stale against a regenerated client is
// how the projection got narrow in the first place; ItemData's MarshalJSON
// already merges AdditionalProperties and omits nil pointers, so the
// round-trip yields precisely what arrived and picks up new fields for free.
//
// Non-scalar values are skipped rather than stringified: a nested object
// rendered as Go syntax is worse than an absent key, because it looks like
// data.
func fieldBag(d client.ItemData) map[string]string {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil
	}
	out := map[string]string{}
	for name, v := range all {
		if structuralFields[name] {
			continue
		}
		switch t := v.(type) {
		case string:
			if t != "" {
				out[name] = t
			}
		case bool:
			out[name] = strconv.FormatBool(t)
		case float64:
			out[name] = strconv.FormatFloat(t, 'f', -1, 64)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FieldNames returns the bibliographic fields an item actually carries,
// sorted. Empty fields are absent, because Zotero stores "" and "unset"
// identically and a caller comparing two items cannot use a distinction the
// server does not keep.
//
// It exists so a type change can be REPORTED rather than merely survived.
// Zotero drops every field the new type does not declare, silently and
// without naming them; diffing this across the write turns that into data
// the caller can act on. Structural keys (creators, tags, collections, the
// dates) are excluded by fieldBag — creators are reported separately and
// the rest never move on a type change.
func FieldNames(d client.ItemData) []string {
	return slices.Sorted(maps.Keys(fieldBag(d)))
}

// relationSetFromClient maps the GET payload's relations into the same
// split-by-owner shape the local reader produces, with BARE item keys on
// both sides so `item read` renders identically with and without --remote.
// It costs no extra HTTP — relations already ride in the item payload.
//
// Titles are left empty — naming a far end needs the local DB, which this
// package deliberately doesn't touch. The CLI fills them in afterwards
// (labelRemoteRelations): the RELATION is what's too new to be local, not
// the papers it points at. Returns nil for a relation-free item so the JSON
// key stays absent.
func relationSetFromClient(m *map[string]client.ItemData_Relations_AdditionalProperties) *local.ItemRelationSet {
	if m == nil {
		return nil
	}
	out := local.ItemRelationSet{}
	for pred, uris := range decodeRelations(*m) {
		keys := lo.FilterMap(uris, func(uri string, _ int) (string, bool) {
			k := keyFromURI(uri)
			return k, k != ""
		})
		if len(keys) == 0 {
			continue
		}
		if pred == RelatedPredicate {
			out.Related = keys
			continue
		}
		if out.Other == nil {
			out.Other = map[string][]string{}
		}
		out.Other[pred] = keys
	}
	if len(out.Related) == 0 && len(out.Other) == 0 {
		return nil
	}
	return &out
}

func creatorFromClient(c client.Creator, idx int) local.Creator {
	out := local.Creator{
		Type:     string(c.CreatorType),
		OrderIdx: idx,
	}
	if c.Name != nil {
		out.Name = strings.TrimSpace(*c.Name)
	}
	if c.FirstName != nil {
		out.First = strings.TrimSpace(*c.FirstName)
	}
	if c.LastName != nil {
		out.Last = strings.TrimSpace(*c.LastName)
	}
	return out
}

// CollectionFromClient converts a Zotero Web API collection into the
// local.Collection shape. ItemCount comes from the API's Meta.NumItems
// (total item count including sub-collection descendants).
func CollectionFromClient(c *client.Collection) local.Collection {
	if c == nil {
		return local.Collection{}
	}
	out := local.Collection{
		Key:  c.Key,
		Name: c.Data.Name,
	}
	if c.Meta != nil && c.Meta.NumItems != nil {
		out.ItemCount = *c.Meta.NumItems
	}
	if c.Data.ParentCollection != nil {
		// oneof(string,false) — try string form.
		var s string
		raw, err := c.Data.ParentCollection.MarshalJSON()
		if err == nil && len(raw) > 2 && raw[0] == '"' {
			s = string(raw[1 : len(raw)-1])
		}
		out.ParentKey = s
	}
	return out
}
