package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
)

// stubAPIServer pretends to be the Zotero Web API for the user library.
// Returns the supplied items on the /users/{userID}/items endpoint and
// 200/empty everywhere else, which is enough for ListItems to short-
// circuit and finish without paginating.
func stubAPIServer(t *testing.T, items []client.Item) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/users/42/items" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(items)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := api.New(&zot.Config{APIKey: "test", UserID: "42"},
		api.WithBaseURL(srv.URL),
		api.WithLibrary(zot.LibraryRef{Scope: zot.LibPersonal, APIPath: "users/42"}))
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	return c
}

func ptrStr(s string) *string { return &s }

func TestRemoteIndex_BuildsDOIMapFromAPI(t *testing.T) {
	t.Parallel()
	items := []client.Item{
		{Key: "AAA00001", Data: client.ItemData{ItemType: "preprint", DOI: ptrStr("10.1/Foo")}},
		{Key: "BBB00002", Data: client.ItemData{ItemType: "journalArticle", DOI: ptrStr("10.2/bar")}},
		{Key: "CCC00003", Data: client.ItemData{ItemType: "preprint"}}, // no DOI
	}
	c := stubAPIServer(t, items)
	idx := RemoteIndex(context.Background(), c)

	hits, err := idx.LookupKeysByDOI([]string{"10.1/foo", "10.2/BAR", "10.404/missing"})
	if err != nil {
		t.Fatal(err)
	}
	if hits["10.1/foo"] != "AAA00001" {
		t.Errorf("case-insensitive hit failed: %+v", hits)
	}
	if hits["10.2/bar"] != "BBB00002" {
		t.Errorf("case-insensitive hit failed: %+v", hits)
	}
	if _, ok := hits["10.404/missing"]; ok {
		t.Errorf("missing DOI should be absent from result: %+v", hits)
	}
}

func TestRemoteIndex_PrefetchOnce(t *testing.T) {
	t.Parallel()
	// Count fetches by spinning our own server (stubAPIServer doesn't
	// expose a counter). Two LookupKeysByDOI calls = 1 prefetch.
	var fetches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/users/1/items" {
			fetches++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]client.Item{
				{Key: "K1", Data: client.ItemData{ItemType: "preprint", DOI: ptrStr("10.1/foo")}},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := api.New(&zot.Config{APIKey: "k", UserID: "1"},
		api.WithBaseURL(srv.URL),
		api.WithLibrary(zot.LibraryRef{Scope: zot.LibPersonal, APIPath: "users/1"}))
	if err != nil {
		t.Fatal(err)
	}
	idx := RemoteIndex(context.Background(), c)

	for range 3 {
		if _, err := idx.LookupKeysByDOI([]string{"10.1/foo"}); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 {
		t.Errorf("fetches = %d, want 1 (sync.Once should cache)", fetches)
	}
}
