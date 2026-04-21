package api

// End-to-end integration tests that round-trip create-collection and
// create-item under each library scope against a fake server. Protects
// the Phase-3 scope dispatch against regression.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/client"
)

// fakeZotero is a tiny in-memory stand-in for the Zotero Web API that
// handles CREATE for collections + items under both user and group paths,
// returning the submission index as a synthesized key. Enough to exercise
// the full request/response round-trip without touching the network.
type fakeZotero struct {
	mu        sync.Mutex
	requests  []string // captured request paths, in order
	nextKey   int
	userID    string
	groupID   string
	userItems []client.ItemData
	grpItems  []client.ItemData
	userColls []client.CollectionData
	grpColls  []client.CollectionData
}

func newFakeZotero(userID, groupID string) *fakeZotero {
	return &fakeZotero{userID: userID, groupID: groupID, nextKey: 1}
}

func (f *fakeZotero) writtenKey() string {
	// Zotero item keys are 8 uppercase alphanumerics; the exact shape
	// doesn't matter for routing tests.
	key := "KEY00000"
	suffix := []byte{'0', '0', '0', '0', '0', '0', '0', '0'}
	n := f.nextKey
	f.nextKey++
	for i := len(suffix) - 1; i >= 0 && n > 0; i-- {
		suffix[i] = byte('0' + n%10)
		n /= 10
	}
	return key[:0] + string(suffix)
}

func (f *fakeZotero) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.Path)

	userItemsPath := "/users/" + f.userID + "/items"
	userCollsPath := "/users/" + f.userID + "/collections"
	grpItemsPath := "/groups/" + f.groupID + "/items"
	grpCollsPath := "/groups/" + f.groupID + "/collections"

	switch {
	case r.Method == http.MethodPost && r.URL.Path == userItemsPath:
		f.acceptItems(w, r, false)
	case r.Method == http.MethodPost && r.URL.Path == grpItemsPath:
		f.acceptItems(w, r, true)
	case r.Method == http.MethodPost && r.URL.Path == userCollsPath:
		f.acceptColls(w, r, false)
	case r.Method == http.MethodPost && r.URL.Path == grpCollsPath:
		f.acceptColls(w, r, true)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeZotero) acceptItems(w http.ResponseWriter, r *http.Request, group bool) {
	var body []client.ItemData
	_ = json.NewDecoder(r.Body).Decode(&body)
	if group {
		f.grpItems = append(f.grpItems, body...)
	} else {
		f.userItems = append(f.userItems, body...)
	}
	successful := map[string]map[string]any{}
	for i := range body {
		successful[idxStr(i)] = map[string]any{"key": f.writtenKey()}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"successful": successful,
		"unchanged":  map[string]string{},
		"failed":     map[string]any{},
	})
}

func (f *fakeZotero) acceptColls(w http.ResponseWriter, r *http.Request, group bool) {
	var body []client.CollectionData
	_ = json.NewDecoder(r.Body).Decode(&body)
	if group {
		f.grpColls = append(f.grpColls, body...)
	} else {
		f.userColls = append(f.userColls, body...)
	}
	successful := map[string]map[string]any{}
	for i := range body {
		successful[idxStr(i)] = map[string]any{"key": f.writtenKey()}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"successful": successful,
		"unchanged":  map[string]string{},
		"failed":     map[string]any{},
	})
}

func idxStr(i int) string { return strconv.Itoa(i) }

func TestIntegration_ScopeRoundTrip(t *testing.T) {
	t.Parallel()
	const userID = "42"
	const groupID = "6506098"

	fake := newFakeZotero(userID, groupID)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	mk := func(scope zot.LibraryScope) *Client {
		ref := zot.LibraryRef{
			Scope:   scope,
			APIPath: "users/" + userID,
			Name:    "Personal",
		}
		if scope == zot.LibShared {
			ref = zot.LibraryRef{
				Scope:   zot.LibShared,
				APIPath: "groups/" + groupID,
				Name:    "sciminds",
			}
		}
		fc := &fakeClock{}
		c, err := New(
			&zot.Config{APIKey: "test", UserID: userID},
			WithBaseURL(srv.URL),
			WithClock(fc.now, fc.sleep),
			WithLibrary(ref),
		)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	ctx := context.Background()

	// Personal: create a collection, then an item.
	personal := mk(zot.LibPersonal)
	if _, err := personal.CreateCollection(ctx, "Personal Papers", ""); err != nil {
		t.Fatalf("personal CreateCollection: %v", err)
	}
	title := "Personal paper"
	if _, err := personal.CreateItem(ctx, client.ItemData{
		ItemType: "journalArticle",
		Title:    &title,
	}); err != nil {
		t.Fatalf("personal CreateItem: %v", err)
	}

	// Shared: same ops under groups/<id>/.
	shared := mk(zot.LibShared)
	if _, err := shared.CreateCollection(ctx, "Shared Papers", ""); err != nil {
		t.Fatalf("shared CreateCollection: %v", err)
	}
	sharedTitle := "Shared paper"
	if _, err := shared.CreateItem(ctx, client.ItemData{
		ItemType: "journalArticle",
		Title:    &sharedTitle,
	}); err != nil {
		t.Fatalf("shared CreateItem: %v", err)
	}

	// Route assertions: paths came in under the expected prefix.
	var userPaths, grpPaths int
	for _, p := range fake.requests {
		switch {
		case strings.HasPrefix(p, "/users/"+userID+"/"):
			userPaths++
		case strings.HasPrefix(p, "/groups/"+groupID+"/"):
			grpPaths++
		default:
			t.Errorf("unexpected path %q — belongs to neither library", p)
		}
	}
	if userPaths != 2 {
		t.Errorf("user-library requests = %d, want 2", userPaths)
	}
	if grpPaths != 2 {
		t.Errorf("group-library requests = %d, want 2", grpPaths)
	}

	// Payload assertions: each library holds exactly its own items and collections.
	if len(fake.userItems) != 1 || fake.userItems[0].Title == nil || *fake.userItems[0].Title != "Personal paper" {
		t.Errorf("user items = %+v, want [Personal paper]", fake.userItems)
	}
	if len(fake.grpItems) != 1 || fake.grpItems[0].Title == nil || *fake.grpItems[0].Title != "Shared paper" {
		t.Errorf("group items = %+v, want [Shared paper]", fake.grpItems)
	}
	if len(fake.userColls) != 1 || fake.userColls[0].Name != "Personal Papers" {
		t.Errorf("user colls = %+v", fake.userColls)
	}
	if len(fake.grpColls) != 1 || fake.grpColls[0].Name != "Shared Papers" {
		t.Errorf("group colls = %+v", fake.grpColls)
	}
}
