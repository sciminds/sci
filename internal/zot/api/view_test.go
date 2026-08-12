package api

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sciminds/sci/internal/zot/client"
)

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

func TestItemFromClient_FieldsIsNilOnlyWhenTheItemCarriesNone(t *testing.T) {
	t.Parallel()
	// An item with a title has a field bag: `title` is a row in itemData
	// on the local side, so it is one here too. Fields goes nil only when
	// the item carries no bibliographic field at all, which keeps the JSON
	// shape minimal without inventing a difference between the planes.
	title := "X"
	it := &client.Item{Key: "Z", Data: client.ItemData{ItemType: "preprint", Title: &title}}
	got := ItemFromClient(it)
	if got.Extra != "" {
		t.Errorf("Extra = %q, want empty", got.Extra)
	}
	if got.Fields["title"] != "X" {
		t.Errorf("Fields[title] = %q, want X", got.Fields["title"])
	}

	bare := ItemFromClient(&client.Item{Key: "Z", Data: client.ItemData{ItemType: "preprint"}})
	if bare.Fields != nil {
		t.Errorf("Fields = %v, want nil when the item carries no fields", bare.Fields)
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

func TestItemFromClient_PopulatesNumChildren(t *testing.T) {
	t.Parallel()
	// meta.numChildren is what the saved-search childless post-filter relies
	// on — Zotero's Web API has no native "no children" filter, so callers
	// project /items/top + filter NumChildren==0 themselves.
	title := "Has children"
	it := &client.Item{
		Key:     "WITHKIDS",
		Version: 1,
		Data:    client.ItemData{ItemType: "journalArticle", Title: &title},
		Meta:    &client.Item_Meta{NumChildren: new(2)},
	}
	got := ItemFromClient(it)
	if got.NumChildren != 2 {
		t.Errorf("NumChildren = %d, want 2", got.NumChildren)
	}
}

func TestItemFromClient_NumChildrenAbsentIsZero(t *testing.T) {
	t.Parallel()
	// Items lacking meta (e.g. minimal test fixtures) must produce zero,
	// not crash. Childless filtering would treat the item as childless,
	// which is the safe interpretation for absent data.
	title := "No meta"
	it := &client.Item{Key: "NOMETA00", Data: client.ItemData{ItemType: "preprint", Title: &title}}
	got := ItemFromClient(it)
	if got.NumChildren != 0 {
		t.Errorf("NumChildren = %d, want 0 when meta is nil", got.NumChildren)
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
			NumItems: new(12),
		},
	}
	got := CollectionFromClient(c)
	if got.Key != "COLL0001" || got.Name != "My Papers" || got.ItemCount != 12 {
		t.Errorf("got %+v", got)
	}
}

// Relations ride along on the --remote path as BARE KEYS, matching what the
// local reader produces, so `item read` renders the same block either way.
// The GET payload already carries them — mapping them costs no extra HTTP.
func TestItemFromClient_MapsRelationsToBareKeys(t *testing.T) {
	t.Parallel()

	rels := map[string][]string{
		RelatedPredicate: {
			"http://zotero.org/users/17450224/items/PAPER001",
			"http://zotero.org/users/17450224/items/PAPER002",
		},
		"owl:sameAs":  {"http://zotero.org/groups/6506098/items/GRPCOPY1"},
		"dc:isPartOf": {"http://example.org/not-an-item"},
	}
	encoded := encodeRelations(rels)
	it := &client.Item{
		Key:  "NOTE0001",
		Data: client.ItemData{ItemType: "note", Relations: &encoded},
	}

	got := ItemFromClient(it)

	if got.Relations == nil {
		t.Fatal("Relations = nil, want the dc:relation pair")
	}
	if want := []string{"PAPER001", "PAPER002"}; !slices.Equal(got.Relations.Related, want) {
		t.Errorf("Related = %v, want %v", got.Relations.Related, want)
	}
	if want := []string{"GRPCOPY1"}; !slices.Equal(got.Relations.Other["owl:sameAs"], want) {
		t.Errorf("Other[owl:sameAs] = %v, want %v", got.Relations.Other["owl:sameAs"], want)
	}
	// A relation object that isn't a Zotero item URI drops out entirely
	// rather than leaving an empty predicate behind.
	if _, ok := got.Relations.Other["dc:isPartOf"]; ok {
		t.Errorf("Other[dc:isPartOf] = %v, want absent", got.Relations.Other["dc:isPartOf"])
	}
}

func TestItemFromClient_NoRelationsLeavesFieldNil(t *testing.T) {
	t.Parallel()

	got := ItemFromClient(&client.Item{Key: "ABC12345", Data: client.ItemData{ItemType: "book"}})
	if got.Relations != nil {
		t.Errorf("Relations = %+v, want nil", got.Relations)
	}

	empty := encodeRelations(map[string][]string{})
	got = ItemFromClient(&client.Item{Key: "ABC12345", Data: client.ItemData{ItemType: "book", Relations: &empty}})
	if got.Relations != nil {
		t.Errorf("Relations = %+v, want nil for an empty relations object", got.Relations)
	}
}

// A remote read is the ground-truth path for verifying a write — the local
// mirror cannot tell a field this CLI just wrote from one that was never
// there. It projected two fields out of the ~66 an item can carry, so it
// could verify `extra` and `citationKey` and nothing else. That cost a real
// false alarm: volume, issue and pages read back absent after a write and
// looked exactly like data loss, when the fields were on the server the
// whole time and the converter was dropping them.
//
// The projection is a JSON round-trip rather than a hand-written list of
// the 76 typed fields on ItemData, because a hand-written list going stale
// against a regenerated client IS this bug.
func TestItemFromClient_ProjectsEveryFieldTheServerSent(t *testing.T) {
	t.Parallel()
	vol, iss, pages := "108", "4", "814-834"
	pub, title := "Psychological Review", "The Emotional Dog"
	it := &client.Item{
		Key:     "FIELDS01",
		Version: 3,
		Data: client.ItemData{
			ItemType:         "journalArticle",
			Title:            &title,
			Volume:           &vol,
			Issue:            &iss,
			Pages:            &pages,
			PublicationTitle: &pub,
		},
	}
	got := ItemFromClient(it)
	for name, want := range map[string]string{
		"volume": vol, "issue": iss, "pages": pages,
		"publicationTitle": pub, "title": title,
	} {
		if got.Fields[name] != want {
			t.Errorf("Fields[%s] = %q, want %q", name, got.Fields[name], want)
		}
	}
}

// Structural data is already typed on local.Item, and the local reader's
// Fields comes from itemData, which holds none of it. Letting creators or
// collections through as stringified JSON would make the two planes
// disagree in exactly the place this change exists to make them agree.
func TestItemFromClient_KeepsStructuralDataOutOfFields(t *testing.T) {
	t.Parallel()
	fn, ln := "Jonathan", "Haidt"
	cols := []string{"COLL0001"}
	added := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	it := &client.Item{
		Key:     "STRUCT01",
		Version: 9,
		Data: client.ItemData{
			ItemType:    "journalArticle",
			Creators:    &[]client.Creator{{CreatorType: "author", FirstName: &fn, LastName: &ln}},
			Collections: &cols,
			Tags:        &[]client.Tag{{Tag: "moral"}},
			DateAdded:   &added,
		},
	}
	got := ItemFromClient(it)
	for _, name := range []string{
		"key", "version", "itemType", "creators", "tags",
		"collections", "relations", "dateAdded", "dateModified",
	} {
		if v, ok := got.Fields[name]; ok {
			t.Errorf("Fields[%s] = %q, want it absent — it is typed on the item", name, v)
		}
	}
}

// The drift guard. This projection went narrow once and stayed narrow
// because nothing compared it against the client it projects from — so
// this walks every *string field the generated ItemData declares, sets it,
// and requires fieldBag to return all of them.
//
// It fails the day `just zot-gen` adds a field the projection would drop,
// which is the failure mode that produced the two-field version.
func TestFieldBagDropsNothingTheGeneratedClientDeclares(t *testing.T) {
	t.Parallel()
	var d client.ItemData
	v := reflect.ValueOf(&d).Elem()
	typ := v.Type()

	want := map[string]string{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.String {
			continue // structural or non-scalar; covered by its own test
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || structuralFields[name] {
			continue
		}
		// Built through reflect.New so named string types (LinkMode and
		// friends) get a *client.X rather than a *string.
		val := "v-" + name
		ptr := reflect.New(f.Type.Elem())
		ptr.Elem().SetString(val)
		v.Field(i).Set(ptr)
		want[name] = val
	}
	if len(want) < 40 {
		t.Fatalf("only %d string fields found on ItemData — the walk is not reaching them", len(want))
	}

	got := fieldBag(d)
	for name, wantVal := range want {
		if got[name] != wantVal {
			t.Errorf("fieldBag dropped %s: got %q, want %q", name, got[name], wantVal)
		}
	}
}

// TestFieldNames_ProjectsOnlyPopulatedBibliographicFields.
//
// This is the input to `item update --type`'s dropped-field report, so what
// it counts as "a field the item carries" decides what a type change is
// said to have destroyed. Structural keys must stay out: creators and tags
// survive a type change and reporting them as dropped would be a false
// alarm on every repair.
func TestFieldNames_ProjectsOnlyPopulatedBibliographicFields(t *testing.T) {
	t.Parallel()
	title := "Socially intelligent machines that learn from humans and help humans learn"
	pub := "Philosophical Transactions of the Royal Society A."
	empty := ""
	first, last := "Judi", "Thfan"
	d := client.ItemData{
		ItemType:  "document",
		Title:     &title,
		Publisher: &pub,
		Place:     &empty, // stored empty — indistinguishable from unset
		Creators:  &[]client.Creator{{CreatorType: "author", FirstName: &first, LastName: &last}},
		Tags:      &[]client.Tag{{Tag: "has-markdown"}},
	}

	got := FieldNames(d)
	want := []string{"publisher", "title"}
	if !slices.Equal(got, want) {
		t.Errorf("FieldNames = %v, want %v", got, want)
	}
}

func TestFieldNames_EmptyItemHasNoFields(t *testing.T) {
	t.Parallel()
	if got := FieldNames(client.ItemData{ItemType: "journalArticle"}); len(got) != 0 {
		t.Errorf("FieldNames = %v, want none", got)
	}
}
