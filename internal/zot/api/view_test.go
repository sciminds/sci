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
