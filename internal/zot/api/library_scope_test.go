package api

// Tests for per-scope URL dispatch: every library-scoped op routes under
// /users/<uid>/... or /groups/<gid>/... based on c.Lib.Scope. They reference symbols
// that do not yet exist:
//   - WithLibrary(zot.LibraryRef) Option
//   - Client.Lib field (replacing Client.UserID for path construction)
//   - ListGroups(ctx) ([]zot.GroupRef, error)
// After Phase 3 lands, every write op must route under /users/<uid>/…
// or /groups/<gid>/… based on the configured scope.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/client"
)

func personalRef() zot.LibraryRef {
	return zot.LibraryRef{Scope: zot.LibPersonal, APIPath: "users/42", Name: "Personal"}
}

func sharedRef() zot.LibraryRef {
	return zot.LibraryRef{Scope: zot.LibShared, APIPath: "groups/6506098", Name: "sciminds"}
}

// captureHandler records every request path it receives so tests can
// assert the URL prefix produced by the scope dispatch. The response
// shape depends on the HTTP method so the same handler can service
// creates (POST), fetches (GET), updates (PATCH), and deletes (DELETE).
type captureHandler struct {
	mu    sync.Mutex
	paths []string
	body  string // override: if set, served for every request
}

// Minimal Item / Collection envelopes sufficient to satisfy the
// generated client's JSON200 decoding. Fields marked required on the
// struct must be present; everything else is optional.
const (
	stubItemBody = `{"key":"ABC12345","version":1,` +
		`"library":{"id":0,"type":"user","name":"t"},` +
		`"data":{"key":"ABC12345","version":1,"itemType":"journalArticle"}}`
	stubCollectionBody = `{"key":"ABC12345","version":1,` +
		`"library":{"id":0,"type":"user","name":"t"},` +
		`"data":{"key":"ABC12345","version":1,"name":"x"}}`
	stubChildrenBody = `[]`
	stubMultiOKBody  = `{"successful":{"0":{"key":"NEWKEY01"}},"unchanged":{},"failed":{}}`
)

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.URL.Path)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if h.body != "" {
		_, _ = w.Write([]byte(h.body))
		return
	}
	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasSuffix(r.URL.Path, "/children"):
			_, _ = w.Write([]byte(stubChildrenBody))
		case strings.Contains(r.URL.Path, "/collections/"):
			_, _ = w.Write([]byte(stubCollectionBody))
		default:
			_, _ = w.Write([]byte(stubItemBody))
		}
	case http.MethodDelete, http.MethodPatch:
		w.WriteHeader(http.StatusNoContent)
	default:
		_, _ = w.Write([]byte(stubMultiOKBody))
	}
}

func (h *captureHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.paths))
	copy(out, h.paths)
	return out
}

func newScopedClient(t *testing.T, handler http.Handler, ref zot.LibraryRef) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	fc := &fakeClock{}
	c, err := New(testCfg(),
		WithBaseURL(srv.URL),
		WithClock(fc.now, fc.sleep),
		WithLibrary(ref),
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClient_PersonalScope_UsesUsersPath(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	c := newScopedClient(t, h, personalRef())
	if _, err := c.CreateCollection(context.Background(), "Test", ""); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	paths := h.snapshot()
	if len(paths) == 0 {
		t.Fatal("no requests captured")
	}
	for _, p := range paths {
		if !strings.HasPrefix(p, "/users/42/") {
			t.Errorf("path %q does not start with /users/42/", p)
		}
	}
}

func TestClient_SharedScope_UsesGroupsPath(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	c := newScopedClient(t, h, sharedRef())
	if _, err := c.CreateCollection(context.Background(), "Test", ""); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	paths := h.snapshot()
	if len(paths) == 0 {
		t.Fatal("no requests captured")
	}
	for _, p := range paths {
		if !strings.HasPrefix(p, "/groups/6506098/") {
			t.Errorf("path %q does not start with /groups/6506098/", p)
		}
	}
}

func TestClient_RequiresLibrary(t *testing.T) {
	t.Parallel()
	h := &captureHandler{}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	fc := &fakeClock{}
	_, err := New(testCfg(), WithBaseURL(srv.URL), WithClock(fc.now, fc.sleep))
	if err == nil {
		t.Fatal("expected error when WithLibrary is not provided")
	}
	if !strings.Contains(err.Error(), "WithLibrary") {
		t.Errorf("err=%v, want mention of WithLibrary", err)
	}
}

// TestAllWriteOps_RespectScope is the belt-and-suspenders table-driven
// guard that every op we care about is dispatched per scope. Add entries
// here as new write ops land. Multi-request ops (GET+PATCH, GET+DELETE)
// are covered by the "every captured path must carry the scope prefix"
// check — both the precursor read and the mutation must route together.
func TestAllWriteOps_RespectScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ops := []struct {
		name string
		call func(c *Client) error
		// which URL fragment (after the scope prefix) we expect to see.
		wantFragment string
	}{
		{
			name: "CreateItem",
			call: func(c *Client) error {
				title := "Scope test"
				_, err := c.CreateItem(ctx, client.ItemData{
					ItemType: "journalArticle",
					Title:    &title,
				})
				return err
			},
			wantFragment: "/items",
		},
		{
			name: "UpdateItem",
			call: func(c *Client) error {
				title := "Patched"
				return c.UpdateItem(ctx, "ABC12345", client.ItemData{Title: &title})
			},
			wantFragment: "/items/ABC12345",
		},
		{
			name:         "TrashItem",
			call:         func(c *Client) error { return c.TrashItem(ctx, "ABC12345") },
			wantFragment: "/items/ABC12345",
		},
		{
			name:         "ListChildren",
			call:         func(c *Client) error { _, err := c.ListChildren(ctx, "ABC12345"); return err },
			wantFragment: "/items/ABC12345/children",
		},
		{
			name:         "CreateCollection",
			call:         func(c *Client) error { _, err := c.CreateCollection(ctx, "T", ""); return err },
			wantFragment: "/collections",
		},
		{
			name:         "DeleteCollection",
			call:         func(c *Client) error { return c.DeleteCollection(ctx, "ABC12345") },
			wantFragment: "/collections/ABC12345",
		},
		{
			name:         "DeleteTagsFromLibrary",
			call:         func(c *Client) error { return c.DeleteTagsFromLibrary(ctx, []string{"a", "b"}) },
			wantFragment: "/tags",
		},
	}

	for _, ref := range []zot.LibraryRef{personalRef(), sharedRef()} {
		ref := ref
		t.Run(string(ref.Scope), func(t *testing.T) {
			for _, op := range ops {
				op := op
				t.Run(op.name, func(t *testing.T) {
					h := &captureHandler{}
					c := newScopedClient(t, h, ref)
					if err := op.call(c); err != nil {
						t.Fatalf("%s: %v", op.name, err)
					}
					paths := h.snapshot()
					if len(paths) == 0 {
						t.Fatalf("%s: no paths captured", op.name)
					}
					scopePrefix := "/" + ref.APIPath + "/"
					wantOpPrefix := "/" + ref.APIPath + op.wantFragment
					var sawOpPath bool
					for _, p := range paths {
						if !strings.HasPrefix(p, scopePrefix) {
							t.Errorf("%s: path %q leaks outside scope %q", op.name, p, scopePrefix)
						}
						if strings.HasPrefix(p, wantOpPrefix) {
							sawOpPath = true
						}
					}
					if !sawOpPath {
						t.Errorf("%s: no captured path matches %q; paths=%v", op.name, wantOpPrefix, paths)
					}
				})
			}
		})
	}
}

// TestListGroups_Endpoint sketches the Web API helper used by the
// lazy-probe + setup auto-detect paths. The endpoint is
// /users/<userID>/groups (always user-rooted — it lists groups the
// account belongs to, not items within a group).
func TestListGroups_Endpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":6506098,"data":{"name":"sciminds","type":"Private","owner":42}}]`))
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	fc := &fakeClock{}
	c, err := New(testCfg(),
		WithBaseURL(srv.URL),
		WithClock(fc.now, fc.sleep),
		WithLibrary(personalRef()),
	)
	if err != nil {
		t.Fatal(err)
	}

	groups, err := c.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if gotPath != "/users/42/groups" {
		t.Errorf("path = %q, want /users/42/groups", gotPath)
	}
	if len(groups) != 1 || groups[0].Name != "sciminds" || groups[0].ID != "6506098" {
		t.Errorf("groups = %+v, want [sciminds/6506098]", groups)
	}
}
