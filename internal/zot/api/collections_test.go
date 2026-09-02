package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestGetCollection_ReturnsObjectAnd404(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedCollection("COLL0001", 3, map[string]any{"name": "Papers", "parentCollection": false})
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	got, err := c.GetCollection(ctx, "COLL0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "COLL0001" || got.Version != 3 || got.Data["name"] != "Papers" {
		t.Errorf("collection = %+v, want COLL0001 v3 named Papers", *got)
	}
	if want := []string{"GET /users/42/collections/COLL0001"}; !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want %v", f.methods(), want)
	}
	if _, err := c.GetCollection(ctx, "NOPE0000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing collection error = %v, want ErrNotFound", err)
	}
}

func TestCreateCollection_TopLevelSendsParentFalse(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	got, err := c.CreateCollection(context.Background(), "Papers", "")
	if err != nil {
		t.Fatal(err)
	}
	req := f.seen()[0]
	if req.Method != http.MethodPost || req.Path != "/users/42/collections" {
		t.Errorf("request = %s %s, want POST /users/42/collections", req.Method, req.Path)
	}
	var body []map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil || len(body) != 1 {
		t.Fatalf("body = %s, want a one-element array", req.Body)
	}
	if body[0]["name"] != "Papers" || body[0]["parentCollection"] != false {
		t.Errorf("payload = %v, want name Papers and parentCollection false", body[0])
	}
	if got.Key == "" || got.Data["name"] != "Papers" {
		t.Errorf("created = %+v, want key and data from successful[\"0\"]", *got)
	}
}

func TestCreateCollection_WithParentSendsKey(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	if _, err := c.CreateCollection(context.Background(), "Sub", "COLL0001"); err != nil {
		t.Fatal(err)
	}
	var body []map[string]any
	_ = json.Unmarshal([]byte(f.seen()[0].Body), &body)
	if body[0]["parentCollection"] != "COLL0001" {
		t.Errorf("parentCollection = %v, want COLL0001", body[0]["parentCollection"])
	}
}

func TestCreateCollection_FailedSlotIsWriteFailedError(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.failNextWrite = "name cannot be empty"
	c, _ := newTestClient(t, f)

	_, err := c.CreateCollection(context.Background(), "", "")
	if _, ok := errors.AsType[*WriteFailedError](err); !ok {
		t.Fatalf("error = %v (%T), want *WriteFailedError", err, err)
	}
}

func TestPatchCollection_VersionedAndRetriesOnce(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedCollection("COLL0001", 3, map[string]any{"name": "Papers", "parentCollection": false})
	f.setConflicts(1)
	c, _ := newTestClient(t, f)

	err := c.PatchCollection(context.Background(), "COLL0001", map[string]any{"name": "Articles", "parentCollection": "COLL0009"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /users/42/collections/COLL0001", "PATCH /users/42/collections/COLL0001",
		"GET /users/42/collections/COLL0001", "PATCH /users/42/collections/COLL0001",
	}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v", f.methods(), want)
	}
	if got := f.seen()[1].Header.Get("If-Unmodified-Since-Version"); got != "3" {
		t.Errorf("first If-Unmodified-Since-Version = %q, want 3", got)
	}
	if got := f.seen()[3].Header.Get("If-Unmodified-Since-Version"); got != "4" {
		t.Errorf("retry If-Unmodified-Since-Version = %q, want the refreshed 4", got)
	}
	coll := f.collection("COLL0001")
	if coll.Data["name"] != "Articles" || coll.Data["parentCollection"] != "COLL0009" {
		t.Errorf("server collection = %v, want the patched name and parent", coll.Data)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.seen()[1].Body), &sent)
	if _, has := sent["itemType"]; has {
		t.Errorf("collection patch carried itemType: %v", sent)
	}
}

func TestPatchCollection_SecondConflictIsVersionConflictError(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedCollection("COLL0001", 3, map[string]any{"name": "Papers"})
	f.setConflicts(2)
	c, _ := newTestClient(t, f)

	err := c.PatchCollection(context.Background(), "COLL0001", map[string]any{"name": "X"})
	verr, ok := errors.AsType[*VersionConflictError](err)
	if !ok || verr.Path != "/users/42/collections/COLL0001" {
		t.Fatalf("error = %v, want *VersionConflictError for /users/42/collections/COLL0001", err)
	}
}

func TestDeleteCollection_VersionedAndRetriesOnce(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedCollection("COLL0001", 3, map[string]any{"name": "Papers"})
	f.setConflicts(1)
	c, _ := newTestClient(t, f)

	if err := c.DeleteCollection(context.Background(), "COLL0001"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /users/42/collections/COLL0001", "DELETE /users/42/collections/COLL0001",
		"GET /users/42/collections/COLL0001", "DELETE /users/42/collections/COLL0001",
	}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v", f.methods(), want)
	}
	if f.collection("COLL0001") != nil {
		t.Error("collection still present after DeleteCollection")
	}
}

func TestCollections_SharedScopeUsesGroupPath(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.prefix = "/groups/777"
	c, _ := newTestClientFor(t, f, shared)

	if _, err := c.CreateCollection(context.Background(), "Group papers", ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"POST /groups/777/collections"}; !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want %v", f.methods(), want)
	}
}
