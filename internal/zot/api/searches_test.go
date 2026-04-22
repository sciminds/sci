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

// searchHandler is a fake Zotero server for saved-search CRUD. Mirrors
// collHandler in spirit; kept separate so the two endpoints' fixtures don't
// drift (different keys, different per-op state).
type searchHandler struct {
	t        *testing.T
	searches map[string]*fakeSearch
	posts    int32
	deletes  int32

	delete412Once bool
	post412Once   bool
}

type fakeSearch struct {
	data    client.SearchData
	version int
}

func newSearchHandler(t *testing.T) *searchHandler {
	return &searchHandler{t: t, searches: map[string]*fakeSearch{}}
}

func (h *searchHandler) seed(key, name string, version int, conds []client.SearchCondition) {
	d := client.SearchData{Name: name, Conditions: conds}
	k := key
	d.Key = &k
	d.Version = &version
	h.searches[key] = &fakeSearch{data: d, version: version}
}

func (h *searchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/users/42/searches":
		wrapped := make([]client.Search, 0, len(h.searches))
		for k, fs := range h.searches {
			wrapped = append(wrapped, client.Search{Key: k, Version: fs.version, Data: fs.data})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wrapped)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/users/42/searches/"):
		key := strings.TrimPrefix(r.URL.Path, "/users/42/searches/")
		fs, ok := h.searches[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.Search{Key: key, Version: fs.version, Data: fs.data})
	case r.Method == http.MethodPost && r.URL.Path == "/users/42/searches":
		atomic.AddInt32(&h.posts, 1)
		body, _ := io.ReadAll(r.Body)
		var batch []client.SearchData
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
			// Update path: payload carries a key.
			if d.Key != nil && *d.Key != "" {
				existing, ok := h.searches[*d.Key]
				if !ok {
					result["failed"].(map[string]any)[itoaIdx(idx)] = map[string]any{"code": 404, "message": "not found"}
					continue
				}
				if h.post412Once {
					h.post412Once = false
					existing.version += 1 // simulate someone else writing in between
					result["failed"].(map[string]any)[itoaIdx(idx)] = map[string]any{"code": 412, "message": "version conflict"}
					continue
				}
				existing.version += 1
				existing.data.Name = d.Name
				existing.data.Conditions = d.Conditions
				v := existing.version
				existing.data.Version = &v
				result["successful"].(map[string]any)[itoaIdx(idx)] = client.Search{
					Key: *d.Key, Version: existing.version, Data: existing.data,
				}
				continue
			}
			// Create path: synthesize a new key.
			key := "SAVED001"
			d.Key = &key
			v := 1
			d.Version = &v
			h.searches[key] = &fakeSearch{data: d, version: 1}
			result["successful"].(map[string]any)[itoaIdx(idx)] = client.Search{
				Key: key, Version: 1, Data: d,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/users/42/searches/"):
		atomic.AddInt32(&h.deletes, 1)
		key := strings.TrimPrefix(r.URL.Path, "/users/42/searches/")
		fs, ok := h.searches[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if h.delete412Once {
			h.delete412Once = false
			fs.version += 1
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		delete(h.searches, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		h.t.Logf("unhandled: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestCreateSavedSearch_Hydrates(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	c, _ := newTestClient(t, h)

	conds := []client.SearchCondition{
		{Condition: "title", Operator: "contains", Value: "hippocampus"},
		{Condition: "itemType", Operator: "is", Value: "journalArticle"},
	}
	got, err := c.CreateSavedSearch(context.Background(), "Brain papers", conds)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("nil hydrated search")
	}
	if got.Key != "SAVED001" {
		t.Errorf("Key = %q", got.Key)
	}
	if got.Data.Name != "Brain papers" {
		t.Errorf("Name = %q", got.Data.Name)
	}
	if len(got.Data.Conditions) != 2 {
		t.Errorf("Conditions = %d, want 2", len(got.Data.Conditions))
	}
	stored, ok := h.searches["SAVED001"]
	if !ok {
		t.Fatal("search not stored")
	}
	if len(stored.data.Conditions) != 2 {
		t.Errorf("stored conditions = %d, want 2", len(stored.data.Conditions))
	}
}

func TestCreateSavedSearch_RequiresNameAndConditions(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	c, _ := newTestClient(t, h)

	if _, err := c.CreateSavedSearch(context.Background(), "", []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}}); err == nil {
		t.Error("expected error for empty name")
	}
	if _, err := c.CreateSavedSearch(context.Background(), "n", nil); err == nil {
		t.Error("expected error for empty conditions")
	}
}

func TestListSavedSearches(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	h.seed("AAAA1111", "first", 5, []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}})
	h.seed("BBBB2222", "second", 6, []client.SearchCondition{{Condition: "tag", Operator: "is", Value: "y"}})
	c, _ := newTestClient(t, h)

	got, err := c.ListSavedSearches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	names := map[string]bool{}
	for _, g := range got {
		names[g.Data.Name] = true
	}
	if !names["first"] || !names["second"] {
		t.Errorf("expected first and second, got %v", names)
	}
}

func TestGetSavedSearch_NotFound(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	c, _ := newTestClient(t, h)

	_, err := c.GetSavedSearch(context.Background(), "MISSING1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v, want 'not found'", err)
	}
}

func TestDeleteSavedSearch(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	h.seed("SAVEDXX1", "to-delete", 10, []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}})
	c, _ := newTestClient(t, h)

	if err := c.DeleteSavedSearch(context.Background(), "SAVEDXX1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.searches["SAVEDXX1"]; ok {
		t.Error("still present after delete")
	}
}

func TestDeleteSavedSearch_VersionRetry(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	h.seed("SAVEDXX1", "to-delete", 10, []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}})
	h.delete412Once = true
	c, _ := newTestClient(t, h)

	if err := c.DeleteSavedSearch(context.Background(), "SAVEDXX1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.searches["SAVEDXX1"]; ok {
		t.Error("still present after retry")
	}
	if h.deletes != 2 {
		t.Errorf("deletes = %d, want 2", h.deletes)
	}
}

func TestUpdateSavedSearch(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	h.seed("SAVEDXX1", "old", 10, []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}})
	c, _ := newTestClient(t, h)

	newConds := []client.SearchCondition{
		{Condition: "title", Operator: "contains", Value: "transformer"},
		{Condition: "itemType", Operator: "is", Value: "journalArticle"},
	}
	if err := c.UpdateSavedSearch(context.Background(), "SAVEDXX1", "new name", newConds); err != nil {
		t.Fatal(err)
	}
	stored := h.searches["SAVEDXX1"]
	if stored.data.Name != "new name" {
		t.Errorf("name = %q, want 'new name'", stored.data.Name)
	}
	if len(stored.data.Conditions) != 2 {
		t.Errorf("conditions = %d, want 2", len(stored.data.Conditions))
	}
	if stored.version != 11 {
		t.Errorf("version = %d, want 11", stored.version)
	}
}

func TestUpdateSavedSearch_VersionRetry(t *testing.T) {
	t.Parallel()
	h := newSearchHandler(t)
	h.seed("SAVEDXX1", "old", 10, []client.SearchCondition{{Condition: "title", Operator: "is", Value: "x"}})
	h.post412Once = true
	c, _ := newTestClient(t, h)

	newConds := []client.SearchCondition{{Condition: "title", Operator: "contains", Value: "x"}}
	if err := c.UpdateSavedSearch(context.Background(), "SAVEDXX1", "new", newConds); err != nil {
		t.Fatal(err)
	}
	// 1st attempt: 412 fail. 2nd attempt: success. POSTs = 2.
	if h.posts != 2 {
		t.Errorf("posts = %d, want 2", h.posts)
	}
}
