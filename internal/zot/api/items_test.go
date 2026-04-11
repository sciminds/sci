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
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/42/items/") && !strings.HasSuffix(r.URL.Path, "/items/"):
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
			key := "NEWKEY00"
			d.Key = &key
			v := 1
			d.Version = &v
			h.items[key] = &fakeItem{data: d, version: 1}
			result["successful"].(map[string]any)[itoaIdx(idx)] = map[string]any{"key": key, "version": 1}
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

func TestCreateItem(t *testing.T) {
	h := newItemHandler(t)
	c, _ := newTestClient(t, h)

	title := "Test paper"
	key, err := c.CreateItem(context.Background(), client.ItemData{
		ItemType: "journalArticle",
		Title:    &title,
	})
	if err != nil {
		t.Fatal(err)
	}
	if key != "NEWKEY00" {
		t.Errorf("key = %q", key)
	}
}

func TestUpdateItem_VersionRetry(t *testing.T) {
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

func TestRemoveItemFromCollection(t *testing.T) {
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
