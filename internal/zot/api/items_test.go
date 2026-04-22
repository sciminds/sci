package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

// itemHandler is a tiny fake Zotero server routing on method + path prefix.
type itemHandler struct {
	t             *testing.T
	items         map[string]*fakeItem // keyed by item key
	versionSeq    int
	post412Once   bool // force first POST /items/<key> to 412
	delete412Once bool
	gets          int32
	posts         int32
	deletes       int32
}

type fakeItem struct {
	data    client.ItemData
	version int
}

func newItemHandler(t *testing.T) *itemHandler {
	return &itemHandler{t: t, items: map[string]*fakeItem{}}
}

func (h *itemHandler) seed(key string, data client.ItemData, version int) {
	d := data
	k := key
	d.Key = &k
	d.Version = &version
	h.items[key] = &fakeItem{data: d, version: version}
}

func (h *itemHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/users/42/items":
		// Library-wide item list. Paginated by ?start&limit in production;
		// the fake returns all rows in one page.
		wrapped := make([]client.Item, 0, len(h.items))
		for k, fi := range h.items {
			wrapped = append(wrapped, client.Item{
				Key:     k,
				Version: fi.version,
				Data:    fi.data,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wrapped)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/42/items/") && !strings.HasSuffix(r.URL.Path, "/items/"):
		atomic.AddInt32(&h.gets, 1)
		key := strings.TrimPrefix(r.URL.Path, "/users/42/items/")
		it, ok := h.items[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		wrapped := client.Item{
			Key:     key,
			Version: it.version,
			Data:    it.data,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wrapped)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/users/42/items/"):
		key := strings.TrimPrefix(r.URL.Path, "/users/42/items/")
		it, ok := h.items[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if h.post412Once {
			h.post412Once = false
			// Advance the version so the next fetch returns a higher one.
			h.versionSeq++
			it.version += 1
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var patch client.ItemData
		if err := json.Unmarshal(body, &patch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if patch.Title != nil {
			it.data.Title = patch.Title
		}
		if patch.DOI != nil {
			it.data.DOI = patch.DOI
		}
		if patch.Collections != nil {
			it.data.Collections = patch.Collections
		}
		if patch.Tags != nil {
			it.data.Tags = patch.Tags
		}
		it.version += 1
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/users/42/items":
		atomic.AddInt32(&h.posts, 1)
		// Batch create: assign keys to any items without one.
		body, _ := io.ReadAll(r.Body)
		var batch []client.ItemData
		if err := json.Unmarshal(body, &batch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		result := map[string]any{
			"failed":     map[string]any{},
			"unchanged":  map[string]any{},
			"successful": map[string]any{},
		}
		for idx, d := range batch {
			if d.Key != nil && *d.Key != "" {
				key := *d.Key
				it, ok := h.items[key]
				if !ok {
					result["failed"].(map[string]any)[itoaIdx(idx)] = map[string]any{"code": 404, "message": "not found"}
					continue
				}
				if d.Version != nil && *d.Version != it.version {
					result["failed"].(map[string]any)[itoaIdx(idx)] = map[string]any{"code": 412, "message": "version conflict"}
					continue
				}
				if d.Title != nil {
					it.data.Title = d.Title
				}
				if d.DOI != nil {
					it.data.DOI = d.DOI
				}
				if d.Collections != nil {
					it.data.Collections = d.Collections
				}
				if d.Tags != nil {
					it.data.Tags = d.Tags
				}
				it.version++
				// Zotero returns the full wrapped Item JSON under successful.{idx}.
				result["successful"].(map[string]any)[itoaIdx(idx)] = client.Item{
					Key:     key,
					Version: it.version,
					Data:    it.data,
				}
				continue
			}
			key := "NEWKEY00"
			d.Key = &key
			v := 1
			d.Version = &v
			h.items[key] = &fakeItem{data: d, version: 1}
			result["successful"].(map[string]any)[itoaIdx(idx)] = client.Item{
				Key:     key,
				Version: 1,
				Data:    d,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/users/42/items/"):
		atomic.AddInt32(&h.deletes, 1)
		key := strings.TrimPrefix(r.URL.Path, "/users/42/items/")
		it, ok := h.items[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if h.delete412Once {
			h.delete412Once = false
			it.version += 1
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		delete(h.items, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		h.t.Logf("unhandled: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func itoaIdx(i int) string { return string(rune('0' + i)) }

func TestGetItem_ReturnsFullItem(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	title := "Successor Representation"
	doi := "10.1523/JNEUROSCI.0151-18.2018"
	h.seed("ABC12345", client.ItemData{
		ItemType: "journalArticle",
		Title:    &title,
		DOI:      &doi,
	}, 42)
	c, _ := newTestClient(t, h)

	got, err := c.GetItem(context.Background(), "ABC12345")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "ABC12345" || got.Version != 42 {
		t.Errorf("got %+v", got)
	}
	if got.Data.Title == nil || *got.Data.Title != title {
		t.Errorf("title not round-tripped: %+v", got.Data.Title)
	}
}

func TestListItems_ReturnsAll(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	t1, t2 := "One", "Two"
	h.seed("AAA11111", client.ItemData{ItemType: "journalArticle", Title: &t1}, 1)
	h.seed("BBB22222", client.ItemData{ItemType: "book", Title: &t2}, 2)
	c, _ := newTestClient(t, h)

	got, err := c.ListItems(context.Background(), ListItemsOptions{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

// TestListItems_CollectionPath_ForwardsItemType pins down the fix for the
// bug where dispatch silently dropped opts.ItemType on the collection-items
// path. The generated client previously lacked itemType/q/tag on the
// /collections/{key}/items endpoint; after regen the params are present
// and dispatch must forward them to the outgoing URL query string.
func TestListItems_CollectionPath_ForwardsItemType(t *testing.T) {
	t.Parallel()
	var gotPath, gotQuery string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestClient(t, h)

	_, err := c.ListItems(context.Background(), ListItemsOptions{
		CollectionKey: "COLL1234",
		ItemType:      "note",
		Limit:         25,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Request must hit the collection-items path, not the library-wide one.
	if !strings.HasSuffix(gotPath, "/collections/COLL1234/items") {
		t.Errorf("path = %q, want .../collections/COLL1234/items", gotPath)
	}
	// And the ItemType must land in the URL query. Earlier behavior silently
	// dropped it — this is the regression guard.
	if !strings.Contains(gotQuery, "itemType=note") {
		t.Errorf("query = %q, want to contain itemType=note", gotQuery)
	}
}

// TestListItems_CollectionPath_ForwardsQuery covers the q/qmode filters on
// the same path — same mechanism, small additional coverage.
func TestListItems_CollectionPath_ForwardsQuery(t *testing.T) {
	t.Parallel()
	var gotQuery string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestClient(t, h)

	_, err := c.ListItems(context.Background(), ListItemsOptions{
		CollectionKey: "COLL1234",
		Query:         "dopamine",
		QMode:         "everything",
		Limit:         25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "q=dopamine") {
		t.Errorf("query = %q, want q=dopamine", gotQuery)
	}
	if !strings.Contains(gotQuery, "qmode=everything") {
		t.Errorf("query = %q, want qmode=everything", gotQuery)
	}
}

func TestGetItem_NotFound(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	c, _ := newTestClient(t, h)
	_, err := c.GetItem(context.Background(), "MISSING1")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want 'not found'", err)
	}
}

func TestCreateItem(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	c, _ := newTestClient(t, h)

	title := "Test paper"
	it, err := c.CreateItem(context.Background(), client.ItemData{
		ItemType: "journalArticle",
		Title:    &title,
	})
	if err != nil {
		t.Fatal(err)
	}
	if it == nil {
		t.Fatal("CreateItem returned nil item")
	}
	if it.Key != "NEWKEY00" {
		t.Errorf("Key = %q", it.Key)
	}
	if it.Version == 0 {
		t.Error("Version not populated from successful response")
	}
	if it.Data.Title == nil || *it.Data.Title != "Test paper" {
		t.Errorf("Data.Title not hydrated: %+v", it.Data.Title)
	}
	if it.Data.ItemType != "journalArticle" {
		t.Errorf("Data.ItemType = %q", it.Data.ItemType)
	}
}

func TestUpdateItem_VersionRetry(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	h.post412Once = true

	c, _ := newTestClient(t, h)
	newTitle := "Updated"
	if err := c.UpdateItem(context.Background(), "ABC12345",
		client.ItemData{ItemType: "journalArticle", Title: &newTitle}); err != nil {
		t.Fatal(err)
	}
	it := h.items["ABC12345"]
	if it.data.Title == nil || *it.data.Title != "Updated" {
		t.Errorf("title not applied: %+v", it.data.Title)
	}
}

func TestTrashItem(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	c, _ := newTestClient(t, h)

	if err := c.TrashItem(context.Background(), "ABC12345"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.items["ABC12345"]; ok {
		t.Error("item still present after trash")
	}
}

func TestTrashItem_VersionRetry(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	h.delete412Once = true
	c, _ := newTestClient(t, h)
	if err := c.TrashItem(context.Background(), "ABC12345"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.items["ABC12345"]; ok {
		t.Error("item still present after retry")
	}
}

func TestAddTagToItem(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	c, _ := newTestClient(t, h)

	if err := c.AddTagToItem(context.Background(), "ABC12345", "ml"); err != nil {
		t.Fatal(err)
	}
	tags := h.items["ABC12345"].data.Tags
	if tags == nil || len(*tags) != 1 || (*tags)[0].Tag != "ml" {
		t.Errorf("tags not applied: %+v", tags)
	}

	// Re-adding is idempotent.
	if err := c.AddTagToItem(context.Background(), "ABC12345", "ml"); err != nil {
		t.Fatal(err)
	}
	tags = h.items["ABC12345"].data.Tags
	if len(*tags) != 1 {
		t.Errorf("idempotent add failed: %+v", tags)
	}
}

func TestRemoveTagFromItem(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{
		ItemType: "journalArticle",
		Tags:     &[]client.Tag{{Tag: "ml"}, {Tag: "brain"}},
	}, 10)
	c, _ := newTestClient(t, h)

	if err := c.RemoveTagFromItem(context.Background(), "ABC12345", "ml"); err != nil {
		t.Fatal(err)
	}
	tags := h.items["ABC12345"].data.Tags
	if len(*tags) != 1 || (*tags)[0].Tag != "brain" {
		t.Errorf("tags after remove: %+v", tags)
	}
}

func TestAddItemToCollection(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	c, _ := newTestClient(t, h)

	if err := c.AddItemToCollection(context.Background(), "ABC12345", "COLLXXX1"); err != nil {
		t.Fatal(err)
	}
	colls := h.items["ABC12345"].data.Collections
	if colls == nil || len(*colls) != 1 || (*colls)[0] != "COLLXXX1" {
		t.Errorf("collections: %+v", colls)
	}
}

func TestUpdateItemsBatch(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	h.seed("DEF67890", client.ItemData{ItemType: "book"}, 5)
	c, _ := newTestClient(t, h)

	title := "Bulk Fixed"
	patches := []ItemPatch{
		{Key: "ABC12345", Data: client.ItemData{Title: &title}},
		{Key: "DEF67890", Data: client.ItemData{Title: &title}},
	}
	results, err := c.UpdateItemsBatch(context.Background(), patches)
	if err != nil {
		t.Fatal(err)
	}
	for k, e := range results {
		if e != nil {
			t.Errorf("key %s: %v", k, e)
		}
	}
	if h.items["ABC12345"].data.Title == nil || *h.items["ABC12345"].data.Title != "Bulk Fixed" {
		t.Errorf("ABC12345 title not applied")
	}
	if h.items["DEF67890"].data.Title == nil || *h.items["DEF67890"].data.Title != "Bulk Fixed" {
		t.Errorf("DEF67890 title not applied")
	}
	// Verify the item type was preserved per-item (patch carried over from source).
	if h.items["DEF67890"].data.ItemType != "book" {
		t.Errorf("DEF67890 itemType clobbered: %v", h.items["DEF67890"].data.ItemType)
	}
	if atomic.LoadInt32(&h.posts) != 1 {
		t.Errorf("want 1 POST, got %d", h.posts)
	}
}

func TestUpdateItemsBatch_SkipsGETWhenPreloaded(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	// Seed items so the POST can match them.
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	h.seed("DEF67890", client.ItemData{ItemType: "book"}, 5)
	c, _ := newTestClient(t, h)

	title := "Preloaded Fix"
	patches := []ItemPatch{
		{Key: "ABC12345", Version: 10, ItemType: "journalArticle", Data: client.ItemData{Title: &title}},
		{Key: "DEF67890", Version: 5, ItemType: "book", Data: client.ItemData{Title: &title}},
	}
	results, err := c.UpdateItemsBatch(context.Background(), patches)
	if err != nil {
		t.Fatal(err)
	}
	for k, e := range results {
		if e != nil {
			t.Errorf("key %s: %v", k, e)
		}
	}
	// The critical assertion: zero GETs because version+itemType were
	// pre-supplied from the local DB.
	if g := atomic.LoadInt32(&h.gets); g != 0 {
		t.Errorf("want 0 GETs (preloaded), got %d", g)
	}
	if h.items["ABC12345"].data.Title == nil || *h.items["ABC12345"].data.Title != "Preloaded Fix" {
		t.Errorf("ABC12345 title not applied")
	}
}

func TestUpdateItemsBatch_412RetryUsesGETOnlyForConflicts(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	// Seed with version 10; pass version 9 (stale) for one item to trigger 412.
	h.seed("ABC12345", client.ItemData{ItemType: "journalArticle"}, 10)
	h.seed("DEF67890", client.ItemData{ItemType: "book"}, 5)
	c, _ := newTestClient(t, h)

	title := "Retry Fix"
	patches := []ItemPatch{
		{Key: "ABC12345", Version: 9, ItemType: "journalArticle", Data: client.ItemData{Title: &title}},
		{Key: "DEF67890", Version: 5, ItemType: "book", Data: client.ItemData{Title: &title}},
	}
	results, err := c.UpdateItemsBatch(context.Background(), patches)
	if err != nil {
		t.Fatal(err)
	}
	// DEF67890 should succeed; ABC12345 should succeed on retry.
	for k, e := range results {
		if e != nil {
			t.Errorf("key %s: %v", k, e)
		}
	}
	// Only 1 GET: the 412 retry for ABC12345. DEF67890 never needed a GET.
	if g := atomic.LoadInt32(&h.gets); g != 1 {
		t.Errorf("want 1 GET (412 retry only), got %d", g)
	}
}

func TestRemoveItemFromCollection(t *testing.T) {
	t.Parallel()
	h := newItemHandler(t)
	h.seed("ABC12345", client.ItemData{
		ItemType:    "journalArticle",
		Collections: &[]string{"COLLXXX1", "COLLYYY2"},
	}, 10)
	c, _ := newTestClient(t, h)

	if err := c.RemoveItemFromCollection(context.Background(), "ABC12345", "COLLXXX1"); err != nil {
		t.Fatal(err)
	}
	colls := h.items["ABC12345"].data.Collections
	if len(*colls) != 1 || (*colls)[0] != "COLLYYY2" {
		t.Errorf("collections after remove: %+v", colls)
	}
}
