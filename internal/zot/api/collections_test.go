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

// collHandler is a tiny fake Zotero server for collection CRUD.
type collHandler struct {
	t             *testing.T
	colls         map[string]*fakeColl
	delete412Once bool
	posts         int32
	deletes       int32
}

type fakeColl struct {
	data    client.CollectionData
	version int
}

func newCollHandler(t *testing.T) *collHandler {
	return &collHandler{t: t, colls: map[string]*fakeColl{}}
}

func (h *collHandler) seed(key, name string, version int) {
	d := client.CollectionData{Name: name}
	k := key
	d.Key = &k
	d.Version = &version
	h.colls[key] = &fakeColl{data: d, version: version}
}

func (h *collHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/42/collections/"):
		key := strings.TrimPrefix(r.URL.Path, "/users/42/collections/")
		c, ok := h.colls[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		wrapped := client.Collection{
			Key:     key,
			Version: c.version,
			Data:    c.data,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wrapped)
	case r.Method == http.MethodPost && r.URL.Path == "/users/42/collections":
		atomic.AddInt32(&h.posts, 1)
		body, _ := io.ReadAll(r.Body)
		var batch []client.CollectionData
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
			key := "COLLNEW1"
			d.Key = &key
			v := 1
			d.Version = &v
			h.colls[key] = &fakeColl{data: d, version: 1}
			result["successful"].(map[string]any)[itoaIdx(idx)] = map[string]any{"key": key, "version": 1}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/users/42/collections/"):
		atomic.AddInt32(&h.deletes, 1)
		key := strings.TrimPrefix(r.URL.Path, "/users/42/collections/")
		c, ok := h.colls[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if h.delete412Once {
			h.delete412Once = false
			c.version += 1
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		delete(h.colls, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		h.t.Logf("unhandled: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestCreateCollection_TopLevel(t *testing.T) {
	t.Parallel()
	h := newCollHandler(t)
	c, _ := newTestClient(t, h)

	key, err := c.CreateCollection(context.Background(), "Papers", "")
	if err != nil {
		t.Fatal(err)
	}
	if key != "COLLNEW1" {
		t.Errorf("key = %q", key)
	}
	if h.posts != 1 {
		t.Errorf("posts = %d, want 1", h.posts)
	}
	// Verify the collection was created with the right name.
	stored, ok := h.colls["COLLNEW1"]
	if !ok {
		t.Fatal("collection not stored")
	}
	if stored.data.Name != "Papers" {
		t.Errorf("name = %q, want Papers", stored.data.Name)
	}
	if stored.data.ParentCollection != nil {
		t.Errorf("parent should be nil for top-level, got %+v", stored.data.ParentCollection)
	}
}

func TestCreateCollection_WithParent(t *testing.T) {
	t.Parallel()
	h := newCollHandler(t)
	c, _ := newTestClient(t, h)

	_, err := c.CreateCollection(context.Background(), "Sub", "PARENTAA")
	if err != nil {
		t.Fatal(err)
	}
	stored := h.colls["COLLNEW1"]
	if stored.data.ParentCollection == nil {
		t.Fatal("parent collection not set")
	}
	// ParentCollection is a oneof union; assert by round-tripping through JSON.
	raw, err := stored.data.ParentCollection.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"PARENTAA"` {
		t.Errorf("parent JSON = %s, want \"PARENTAA\"", raw)
	}
}

func TestGetCollection_NotFound(t *testing.T) {
	t.Parallel()
	h := newCollHandler(t)
	c, _ := newTestClient(t, h)

	_, err := c.getCollectionRaw(context.Background(), "MISSING1")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want 'not found'", err)
	}
}

func TestDeleteCollection(t *testing.T) {
	t.Parallel()
	h := newCollHandler(t)
	h.seed("COLLXXX1", "Papers", 10)
	c, _ := newTestClient(t, h)

	if err := c.DeleteCollection(context.Background(), "COLLXXX1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.colls["COLLXXX1"]; ok {
		t.Error("collection still present after delete")
	}
}

func TestDeleteCollection_VersionRetry(t *testing.T) {
	t.Parallel()
	h := newCollHandler(t)
	h.seed("COLLXXX1", "Papers", 10)
	h.delete412Once = true
	c, _ := newTestClient(t, h)

	if err := c.DeleteCollection(context.Background(), "COLLXXX1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.colls["COLLXXX1"]; ok {
		t.Error("collection still present after retry")
	}
	// Delete called twice: first 412, second succeeds.
	if h.deletes != 2 {
		t.Errorf("deletes = %d, want 2", h.deletes)
	}
}
