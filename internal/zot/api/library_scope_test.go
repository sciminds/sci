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
// assert the URL prefix produced by the scope dispatch.
type captureHandler struct {
	mu    sync.Mutex
	paths []string
	body  string // JSON for a successful single-item/collection create
}

func (h *captureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.URL.Path)
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if h.body != "" {
		_, _ = w.Write([]byte(h.body))
		return
	}
	_, _ = w.Write([]byte(`{"successful":{"0":{"key":"NEWKEY01"}},"unchanged":{},"failed":{}}`))
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
// here as new write ops land.
func TestAllWriteOps_RespectScope(t *testing.T) {
	t.Parallel()
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
				_, err := c.CreateItem(context.Background(), client.ItemData{
					ItemType: "journalArticle",
					Title:    &title,
				})
				return err
			},
			wantFragment: "/items",
		},
		{
			name:         "CreateCollection",
			call:         func(c *Client) error { _, err := c.CreateCollection(context.Background(), "T", ""); return err },
			wantFragment: "/collections",
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
					wantPrefix := "/" + ref.APIPath + op.wantFragment
					for _, p := range paths {
						if !strings.HasPrefix(p, wantPrefix) {
							t.Errorf("%s: path %q, want prefix %q", op.name, p, wantPrefix)
						}
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
