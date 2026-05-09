package api

import (
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/local"
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
	// Seed Fields with the entries citekey.Resolve consults so the same
	// enrichment helper works on items from either side of the
	// reads-local / writes-cloud split. Skip empty strings so the JSON
	// shape stays minimal — pointers from the OpenAPI client are
	// frequently non-nil pointing at "" for absent fields.
	seedField := func(name string, p *string) {
		if p == nil || *p == "" {
			return
		}
		if out.Fields == nil {
			out.Fields = map[string]string{}
		}
		out.Fields[name] = *p
	}
	seedField("extra", d.Extra)
	seedField("citationKey", d.CitationKey)
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
	return out
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
