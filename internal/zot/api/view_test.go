package api

import (
	"testing"
	"time"

	"github.com/sciminds/cli/internal/zot/client"
)

func intPtr(i int) *int { return &i }

func TestItemFromClient_MapsCoreFields(t *testing.T) {
	t.Parallel()
	dateAdded := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	fn, ln := "Samuel J.", "Gershman"
	title := "The Successor Representation"
	doi := "10.1523/JNEUROSCI.0151-18.2018"
	cols := []string{"COLL0001", "COLL0002"}

	it := &client.Item{
		Key:     "ABC12345",
		Version: 42,
		Data: client.ItemData{
			ItemType:  "journalArticle",
			Title:     &title,
			DOI:       &doi,
			DateAdded: &dateAdded,
			Creators: &[]client.Creator{
				{CreatorType: "author", FirstName: &fn, LastName: &ln},
			},
			Collections: &cols,
			Tags:        &[]client.Tag{{Tag: "neuroscience"}},
		},
	}

	got := ItemFromClient(it)

	if got.Key != "ABC12345" {
		t.Errorf("Key = %q, want ABC12345", got.Key)
	}
	if got.Type != "journalArticle" {
		t.Errorf("Type = %q, want journalArticle", got.Type)
	}
	if got.Version != 42 {
		t.Errorf("Version = %d, want 42", got.Version)
	}
	if got.Title != "The Successor Representation" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.DOI != doi {
		t.Errorf("DOI = %q", got.DOI)
	}
	if got.DateAdded != "2024-01-15T12:00:00Z" {
		t.Errorf("DateAdded = %q", got.DateAdded)
	}
	if len(got.Creators) != 1 || got.Creators[0].Last != "Gershman" {
		t.Errorf("Creators = %+v", got.Creators)
	}
	if len(got.Collections) != 2 || got.Collections[0] != "COLL0001" {
		t.Errorf("Collections = %v", got.Collections)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "neuroscience" {
		t.Errorf("Tags = %v", got.Tags)
	}
}

func TestItemFromClient_PopulatesExtraAndCitationKey(t *testing.T) {
	t.Parallel()
	extra := "OpenAlex: W123\nCitation Key: hand-pinned\n"
	ck := "explicit-zot7-key"
	it := &client.Item{
		Key:     "EXT00001",
		Version: 1,
		Data: client.ItemData{
			ItemType:    "preprint",
			Extra:       &extra,
			CitationKey: &ck,
		},
	}
	got := ItemFromClient(it)
	if got.Extra != extra {
		t.Errorf("Extra = %q", got.Extra)
	}
	// Fields seeded so downstream citekey.Resolve sees the same data
	// the local-DB Read path provides.
	if got.Fields["extra"] != extra {
		t.Errorf("Fields[extra] = %q", got.Fields["extra"])
	}
	if got.Fields["citationKey"] != ck {
		t.Errorf("Fields[citationKey] = %q", got.Fields["citationKey"])
	}
}

func TestItemFromClient_NoExtraNoFields(t *testing.T) {
	t.Parallel()
	title := "X"
	it := &client.Item{Key: "Z", Data: client.ItemData{ItemType: "preprint", Title: &title}}
	got := ItemFromClient(it)
	if got.Extra != "" {
		t.Errorf("Extra = %q, want empty", got.Extra)
	}
	if got.Fields != nil {
		t.Errorf("Fields = %v, want nil when no extra/citationKey", got.Fields)
	}
}

func TestItemFromClient_EmptyStringFieldsIgnored(t *testing.T) {
	t.Parallel()
	// The OpenAPI client routinely returns non-nil pointers to "" for
	// absent string fields. Don't pollute the JSON output with them.
	empty := ""
	it := &client.Item{Key: "Z", Data: client.ItemData{
		ItemType:    "preprint",
		Extra:       &empty,
		CitationKey: &empty,
	}}
	got := ItemFromClient(it)
	if got.Fields != nil {
		t.Errorf("Fields = %v, want nil when both are empty pointers", got.Fields)
	}
	if got.Extra != "" {
		t.Errorf("Extra = %q, want empty", got.Extra)
	}
}

func TestItemFromClient_HandlesNilSafely(t *testing.T) {
	t.Parallel()
	got := ItemFromClient(nil)
	if got.Key != "" || got.Version != 0 {
		t.Errorf("zero-value not produced: %+v", got)
	}
}

func TestCollectionFromClient_MapsNameAndCount(t *testing.T) {
	t.Parallel()
	c := &client.Collection{
		Key:     "COLL0001",
		Version: 7,
		Data: client.CollectionData{
			Name: "My Papers",
		},
		Meta: &client.Collection_Meta{
			NumItems: intPtr(12),
		},
	}
	got := CollectionFromClient(c)
	if got.Key != "COLL0001" || got.Name != "My Papers" || got.ItemCount != 12 {
		t.Errorf("got %+v", got)
	}
}
