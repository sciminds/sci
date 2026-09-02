package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"

	"github.com/samber/lo"
)

func TestGetItem_ReturnsKeyVersionAndData(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "journalArticle", "title": "Minds"})
	c, _ := newTestClient(t, f)

	got, err := c.GetItem(context.Background(), "ABCD1234")
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != "ABCD1234" || got.Version != 7 {
		t.Errorf("Object = {%s %d}, want {ABCD1234 7}", got.Key, got.Version)
	}
	if got.Data["title"] != "Minds" || got.Data["itemType"] != "journalArticle" {
		t.Errorf("Data = %v, want title/itemType from the server", got.Data)
	}
}

func TestGetItem_404IsErrNotFound(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	_, err := c.GetItem(context.Background(), "MISSING1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestListChildren_ReturnsChildObjects(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("PARENT01", 1, map[string]any{"itemType": "book"})
	f.seedItem("CHILD001", 2, map[string]any{"itemType": "note", "parentItem": "PARENT01"})
	f.seedItem("OTHER001", 3, map[string]any{"itemType": "note"})
	c, _ := newTestClient(t, f)

	kids, err := c.ListChildren(context.Background(), "PARENT01")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].Key != "CHILD001" || kids[0].Version != 2 {
		t.Errorf("children = %+v, want just CHILD001 v2", kids)
	}
	if want := []string{"GET /users/42/items/PARENT01/children"}; !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want %v", f.methods(), want)
	}
}

func TestCreateItem_PostsArrayAndMaterializesSlotZero(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	got, err := c.CreateItem(context.Background(), map[string]any{"itemType": "book", "title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	req := f.seen()[0]
	if req.Method != http.MethodPost || req.Path != "/users/42/items" {
		t.Errorf("request = %s %s, want POST /users/42/items", req.Method, req.Path)
	}
	var body []map[string]any
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil || len(body) != 1 || body[0]["title"] != "T" {
		t.Errorf("body = %s, want a one-element array holding the item", req.Body)
	}
	if got.Key == "" || got.Version == 0 || got.Data["title"] != "T" {
		t.Errorf("created = %+v, want key, version and data filled from successful[\"0\"]", *got)
	}
	if f.item(got.Key) == nil {
		t.Errorf("server has no item %s after create", got.Key)
	}
}

func TestCreateItem_FailedSlotIsWriteFailedError(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.failNextWrite = "'title' is required"
	c, _ := newTestClient(t, f)

	_, err := c.CreateItem(context.Background(), map[string]any{"itemType": "book"})
	werr, ok := errors.AsType[*WriteFailedError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want *WriteFailedError", err, err)
	}
	if werr.Code != 400 || werr.Message != "'title' is required" {
		t.Errorf("WriteFailedError = %+v, want code 400 and the server's message", *werr)
	}
}

func TestCreateItem_413IsWriteFailedError(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
	})
	c, _ := newTestClient(t, h)

	_, err := c.CreateItem(context.Background(), map[string]any{"itemType": "note", "note": "huge"})
	werr, ok := errors.AsType[*WriteFailedError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want *WriteFailedError", err, err)
	}
	if werr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Code = %d, want 413", werr.Code)
	}
}

func TestPatchItem_SendsVersionHeaderAndFillsItemType(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "title": "Old"})
	c, _ := newTestClient(t, f)

	if err := c.PatchItem(context.Background(), "ABCD1234", map[string]any{"title": "New"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /users/42/items/ABCD1234", "PATCH /users/42/items/ABCD1234"}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v", f.methods(), want)
	}
	patch := f.seen()[1]
	if got := patch.Header.Get("If-Unmodified-Since-Version"); got != "7" {
		t.Errorf("If-Unmodified-Since-Version = %q, want 7", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(patch.Body), &sent); err != nil {
		t.Fatal(err)
	}
	if sent["itemType"] != "book" || sent["title"] != "New" {
		t.Errorf("patch body = %v, want itemType filled from the server and the caller's title", sent)
	}
	if f.item("ABCD1234").Data["title"] != "New" {
		t.Errorf("server title = %v, want New", f.item("ABCD1234").Data["title"])
	}
}

func TestPatchItem_KeepsCallerItemType(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	c, _ := newTestClient(t, f)

	err := c.PatchItem(context.Background(), "ABCD1234", map[string]any{"itemType": "bookSection"})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.seen()[1].Body), &sent)
	if sent["itemType"] != "bookSection" {
		t.Errorf("itemType sent = %v, want the caller's bookSection", sent["itemType"])
	}
}

func TestPatchItem_412RefetchesAndRetriesOnce(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f.setConflicts(1)
	c, _ := newTestClient(t, f)

	if err := c.PatchItem(context.Background(), "ABCD1234", map[string]any{"title": "New"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /users/42/items/ABCD1234", "PATCH /users/42/items/ABCD1234",
		"GET /users/42/items/ABCD1234", "PATCH /users/42/items/ABCD1234",
	}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v", f.methods(), want)
	}
	if got := f.seen()[3].Header.Get("If-Unmodified-Since-Version"); got != "8" {
		t.Errorf("retry If-Unmodified-Since-Version = %q, want the refreshed 8", got)
	}
}

func TestPatchItem_SecondConflictIsVersionConflictError(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f.setConflicts(2)
	c, _ := newTestClient(t, f)

	err := c.PatchItem(context.Background(), "ABCD1234", map[string]any{"title": "New"})
	verr, ok := errors.AsType[*VersionConflictError](err)
	if !ok {
		t.Fatalf("error = %v (%T), want *VersionConflictError", err, err)
	}
	if verr.Path != "/users/42/items/ABCD1234" {
		t.Errorf("Path = %q, want /users/42/items/ABCD1234", verr.Path)
	}
	if n := len(f.methods()); n != 4 {
		t.Errorf("%d requests, want exactly 4 (GET, PATCH, GET, PATCH)", n)
	}
}

func TestPatchItem_MissingItemIsErrNotFound(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)

	err := c.PatchItem(context.Background(), "MISSING1", map[string]any{"title": "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestTrashItem_PatchesDeletedFlagAndKeepsItem(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "title": "Keep me"})
	c, _ := newTestClient(t, f)

	changed, err := c.TrashItem(context.Background(), "ABCD1234")
	if err != nil || !changed {
		t.Fatalf("(changed, err) = (%v, %v), want (true, nil)", changed, err)
	}
	want := []string{"GET /users/42/items/ABCD1234", "PATCH /users/42/items/ABCD1234"}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v (trash is item state, never a DELETE)", f.methods(), want)
	}
	patch := f.seen()[1]
	if got := patch.Header.Get("If-Unmodified-Since-Version"); got != "7" {
		t.Errorf("If-Unmodified-Since-Version = %q, want 7", got)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(patch.Body), &sent)
	if sent["deleted"] != float64(1) || sent["itemType"] != "book" {
		t.Errorf("patch body = %v, want {deleted: 1, itemType: book}", sent)
	}
	item := f.item("ABCD1234")
	if item == nil {
		t.Fatal("item gone from the server after TrashItem; trash must not delete")
	}
	if item.Data["deleted"] != float64(1) || item.Data["title"] != "Keep me" {
		t.Errorf("server item = %v, want deleted=1 with every other field intact", item.Data)
	}
}

func TestTrashItem_NoOpWhenAlreadyTrashed(t *testing.T) {
	t.Parallel()
	for name, flag := range map[string]any{"numeric": float64(1), "boolean": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeZotero(t)
			f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "deleted": flag})
			c, _ := newTestClient(t, f)

			changed, err := c.TrashItem(context.Background(), "ABCD1234")
			if err != nil || changed {
				t.Fatalf("(changed, err) = (%v, %v), want (false, nil)", changed, err)
			}
			if n := len(f.methods()); n != 1 {
				t.Errorf("%d requests, want just the GET", n)
			}
		})
	}
}

func TestTrashItem_412RetriesOnceThenConflicts(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f.setConflicts(1)
	c, _ := newTestClient(t, f)
	if _, err := c.TrashItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatalf("one conflict should be absorbed, got %v", err)
	}
	if n := len(f.methods()); n != 4 {
		t.Errorf("%d requests, want 4 (GET, PATCH 412, GET, PATCH)", n)
	}

	f2 := newFakeZotero(t)
	f2.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f2.setConflicts(2)
	c2, _ := newTestClient(t, f2)
	_, err := c2.TrashItem(context.Background(), "ABCD1234")
	if _, ok := errors.AsType[*VersionConflictError](err); !ok {
		t.Fatalf("error = %v, want *VersionConflictError after the second 412", err)
	}
}

func TestDeleteItem_DeletesWithVersionHeader(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	c, _ := newTestClient(t, f)

	if err := c.DeleteItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /users/42/items/ABCD1234", "DELETE /users/42/items/ABCD1234"}
	if !equalStrings(f.methods(), want) {
		t.Fatalf("requests = %v, want %v", f.methods(), want)
	}
	if got := f.seen()[1].Header.Get("If-Unmodified-Since-Version"); got != "7" {
		t.Errorf("If-Unmodified-Since-Version = %q, want 7", got)
	}
	if f.item("ABCD1234") != nil {
		t.Error("item still present on the server after DeleteItem")
	}
}

func TestDeleteItem_412RetriesOnceThenConflicts(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f.setConflicts(1)
	c, _ := newTestClient(t, f)
	if err := c.DeleteItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatalf("one conflict should be absorbed, got %v", err)
	}
	if n := len(f.methods()); n != 4 {
		t.Errorf("%d requests, want 4 (GET, DELETE 412, GET, DELETE)", n)
	}

	f2 := newFakeZotero(t)
	f2.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	f2.setConflicts(2)
	c2, _ := newTestClient(t, f2)
	err := c2.DeleteItem(context.Background(), "ABCD1234")
	if _, ok := errors.AsType[*VersionConflictError](err); !ok {
		t.Fatalf("error = %v, want *VersionConflictError after the second 412", err)
	}
}

func TestAddToCollection_AppendsToServerCopy(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "collections": []any{"COLL0001"}})
	c, _ := newTestClient(t, f)

	changed, err := c.AddToCollection(context.Background(), "ABCD1234", "COLL0002")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	got := f.item("ABCD1234").Data["collections"]
	if want := []any{"COLL0001", "COLL0002"}; !reflect.DeepEqual(got, want) {
		t.Errorf("collections = %v, want %v", got, want)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(f.seen()[1].Body), &sent)
	if _, hasTitle := sent["title"]; hasTitle || sent["itemType"] != "book" {
		t.Errorf("patch = %v, want only collections plus itemType", sent)
	}
}

func TestAddToCollection_NoOpWhenAlreadyMember(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "collections": []any{"COLL0001"}})
	c, _ := newTestClient(t, f)

	changed, err := c.AddToCollection(context.Background(), "ABCD1234", "COLL0001")
	if err != nil || changed {
		t.Fatalf("(changed, err) = (%v, %v), want (false, nil)", changed, err)
	}
	if want := []string{"GET /users/42/items/ABCD1234"}; !equalStrings(f.methods(), want) {
		t.Errorf("requests = %v, want just the GET", f.methods())
	}
}

func TestRemoveFromCollection_RemovesAndNoOps(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "collections": []any{"COLL0001", "COLL0002"}})
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	changed, err := c.RemoveFromCollection(ctx, "ABCD1234", "COLL0001")
	if err != nil || !changed {
		t.Fatalf("(changed, err) = (%v, %v), want (true, nil)", changed, err)
	}
	if got, want := f.item("ABCD1234").Data["collections"], []any{"COLL0002"}; !reflect.DeepEqual(got, want) {
		t.Errorf("collections = %v, want %v", got, want)
	}

	changed, err = c.RemoveFromCollection(ctx, "ABCD1234", "COLL0001")
	if err != nil || changed {
		t.Fatalf("second remove: (changed, err) = (%v, %v), want (false, nil)", changed, err)
	}
}

func TestAddTag_PreservesExistingEntriesVerbatim(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	existing := []any{map[string]any{"tag": "auto", "type": float64(1)}}
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "tags": existing})
	c, _ := newTestClient(t, f)

	changed, err := c.AddTag(context.Background(), "ABCD1234", "to-read")
	if err != nil || !changed {
		t.Fatalf("(changed, err) = (%v, %v), want (true, nil)", changed, err)
	}
	got := f.item("ABCD1234").Data["tags"]
	want := []any{
		map[string]any{"tag": "auto", "type": float64(1)},
		map[string]any{"tag": "to-read"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestAddTag_NoOpWhenPresent(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "tags": []any{map[string]any{"tag": "x"}}})
	c, _ := newTestClient(t, f)

	changed, err := c.AddTag(context.Background(), "ABCD1234", "x")
	if err != nil || changed {
		t.Fatalf("(changed, err) = (%v, %v), want (false, nil)", changed, err)
	}
	if n := len(f.methods()); n != 1 {
		t.Errorf("%d requests, want just the GET", n)
	}
}

func TestRemoveTag_RemovesAndNoOps(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "tags": []any{
		map[string]any{"tag": "keep", "type": float64(1)},
		map[string]any{"tag": "drop"},
	}})
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	changed, err := c.RemoveTag(ctx, "ABCD1234", "drop")
	if err != nil || !changed {
		t.Fatalf("(changed, err) = (%v, %v), want (true, nil)", changed, err)
	}
	got := f.item("ABCD1234").Data["tags"]
	if want := []any{map[string]any{"tag": "keep", "type": float64(1)}}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}

	changed, err = c.RemoveTag(ctx, "ABCD1234", "drop")
	if err != nil || changed {
		t.Fatalf("second remove: (changed, err) = (%v, %v), want (false, nil)", changed, err)
	}
}

func TestAddToCollection_RederivesFromFreshCopyAfter412(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book", "collections": []any{"COLL0001"}})
	// The first PATCH fails with 412 because another writer added COLL0009
	// in between. The re-read must see it, and the retry must keep it.
	f.setConflicts(1)
	f.onConflict = func(obj *fakeObject) { obj.Data["collections"] = []any{"COLL0001", "COLL0009"} }
	c, _ := newTestClient(t, f)

	changed, err := c.AddToCollection(context.Background(), "ABCD1234", "COLL0002")
	if err != nil || !changed {
		t.Fatalf("(changed, err) = (%v, %v), want (true, nil)", changed, err)
	}
	got := f.item("ABCD1234").Data["collections"]
	if want := []any{"COLL0001", "COLL0009", "COLL0002"}; !reflect.DeepEqual(got, want) {
		t.Errorf("collections = %v, want the other writer's COLL0009 kept: %v", got, want)
	}
}

// TestFindItemsByDOISince: the server hands back everything newer than the
// mirror's version and the exact verdict is client-side — the DOI field or
// a "DOI: …" line in extra, case aside — with trashed items never counting
// as the paper being present.
func TestFindItemsByDOISince(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("FIELD001", 101, map[string]any{"itemType": "journalArticle", "DOI": "10.1038/NATURE12373", "title": "By field"})
	f.seedItem("EXTRA001", 102, map[string]any{"itemType": "book", "extra": "Citation Key: x\nDOI: 10.1038/nature12373\n", "title": "By extra"})
	f.seedItem("SUBSTR01", 103, map[string]any{"itemType": "journalArticle", "DOI": "10.1038/nature12373.s001", "title": "Longer DOI"})
	f.seedItem("TRASH001", 104, map[string]any{"itemType": "journalArticle", "DOI": "10.1038/nature12373", "deleted": 1})
	f.seedItem("OTHER001", 105, map[string]any{"itemType": "journalArticle", "DOI": "10.1000/other"})
	f.seedItem("OLDER001", 50, map[string]any{"itemType": "journalArticle", "DOI": "10.1038/nature12373", "title": "Already in the mirror"})
	c, _ := newTestClient(t, f)

	got, err := c.FindItemsByDOISince(context.Background(), "10.1038/Nature12373", 100)
	if err != nil {
		t.Fatal(err)
	}
	keys := lo.Map(got, func(o Object, _ int) string { return o.Key })
	slices.Sort(keys)
	if want := []string{"EXTRA001", "FIELD001"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v (field and extra-line matches, case aside; no substring, no trashed, nothing at or below since)", keys, want)
	}
	req := f.seen()[0]
	if req.Method != http.MethodGet || req.Path != "/users/42/items" {
		t.Errorf("request = %s %s, want GET /users/42/items", req.Method, req.Path)
	}
	q, err := url.ParseQuery(req.Query)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("since") != "100" || q.Get("itemType") != "-attachment" || q.Get("format") != "json" || q.Get("limit") != "100" || q.Get("start") != "0" {
		t.Errorf("query = %s, want since=100&itemType=-attachment&format=json&limit=100&start=0", req.Query)
	}
	if q.Has("q") || q.Has("qmode") {
		t.Errorf("query = %s: q/qmode must not be used, Zotero's q never matches the DOI field", req.Query)
	}
}

// TestFindItemsByDOISince_PagesUntilAShortPage pins the paging: with the
// page size at 2 and five newer items, three pages are fetched and the
// match on the last page is found. Not parallel: it pins findPageSize.
func TestFindItemsByDOISince_PagesUntilAShortPage(t *testing.T) {
	prev := findPageSize
	findPageSize = 2
	t.Cleanup(func() { findPageSize = prev })

	f := newFakeZotero(t)
	for i, key := range []string{"AAAA0001", "BBBB0002", "CCCC0003", "DDDD0004"} {
		f.seedItem(key, 10+i, map[string]any{"itemType": "journalArticle", "DOI": "10.1000/filler"})
	}
	f.seedItem("EEEE0005", 20, map[string]any{"itemType": "journalArticle", "DOI": "10.1038/nature12373"})
	c, _ := newTestClient(t, f)

	got, err := c.FindItemsByDOISince(context.Background(), "10.1038/nature12373", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "EEEE0005" {
		t.Errorf("got %v, want the match from the last page", got)
	}
	starts := lo.Map(f.seen(), func(r recordedRequest, _ int) string {
		q, _ := url.ParseQuery(r.Query)
		return q.Get("start")
	})
	if want := []string{"0", "2", "4"}; !slices.Equal(starts, want) {
		t.Errorf("page starts = %v, want %v (stop on the first page shorter than the limit)", starts, want)
	}
}

func TestFindItemsByDOISince_NoMatchIsEmptyNotError(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)
	got, err := c.FindItemsByDOISince(context.Background(), "10.1000/nothing", 0)
	if err != nil || len(got) != 0 {
		t.Errorf("got (%v, %v), want an empty list", got, err)
	}
}
