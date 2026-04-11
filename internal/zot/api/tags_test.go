package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// tagHandler records every DELETE /tags request it sees, so tests can
// assert on batching.
type tagHandler struct {
	mu       sync.Mutex
	requests []string // Tag query-param values (pipe-separated)
	fail412  bool
	failCode int // non-zero → return this status on first request
}

func (h *tagHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete || r.URL.Path != "/users/42/tags" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.mu.Lock()
	h.requests = append(h.requests, r.URL.Query().Get("tag"))
	code := h.failCode
	h.failCode = 0
	h.mu.Unlock()
	if h.fail412 {
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	if code != 0 {
		w.WriteHeader(code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestDeleteTags_Empty(t *testing.T) {
	h := &tagHandler{}
	c, _ := newTestClient(t, h)

	if err := c.DeleteTagsFromLibrary(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Errorf("expected no requests for empty slice, got %d", len(h.requests))
	}
}

func TestDeleteTags_SingleBatch(t *testing.T) {
	h := &tagHandler{}
	c, _ := newTestClient(t, h)

	if err := c.DeleteTagsFromLibrary(context.Background(), []string{"a", "b", "c"}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(h.requests))
	}
	if h.requests[0] != "a || b || c" {
		t.Errorf("tag = %q, want 'a || b || c'", h.requests[0])
	}
}

func TestDeleteTags_Batching50(t *testing.T) {
	h := &tagHandler{}
	c, _ := newTestClient(t, h)

	// 120 tags → 3 batches (50, 50, 20).
	tags := make([]string, 120)
	for i := range tags {
		tags[i] = "t" + itoaIdx(i%10) // content doesn't matter, just distinct-ish
	}
	if err := c.DeleteTagsFromLibrary(context.Background(), tags); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(h.requests))
	}
	// First two batches must contain exactly 50 tags (49 separators).
	for i, req := range h.requests[:2] {
		if n := strings.Count(req, " || "); n != 49 {
			t.Errorf("batch %d separator count = %d, want 49", i, n)
		}
	}
	// Third batch: 20 tags → 19 separators.
	if n := strings.Count(h.requests[2], " || "); n != 19 {
		t.Errorf("batch 2 separator count = %d, want 19", n)
	}
}

func TestDeleteTags_412ReturnsError(t *testing.T) {
	h := &tagHandler{fail412: true}
	c, _ := newTestClient(t, h)

	err := c.DeleteTagsFromLibrary(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error on 412")
	}
	if !strings.Contains(err.Error(), "library has been modified") {
		t.Errorf("err = %v, want 'library has been modified'", err)
	}
}

func TestDeleteTags_ServerError(t *testing.T) {
	// 400 from the server — not retried by middleware, surfaced as error.
	h := &tagHandler{failCode: http.StatusBadRequest}
	c, _ := newTestClient(t, h)

	err := c.DeleteTagsFromLibrary(context.Background(), []string{"x"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if !strings.Contains(err.Error(), "DELETE /tags") {
		t.Errorf("err = %v, want 'DELETE /tags' prefix", err)
	}
}
