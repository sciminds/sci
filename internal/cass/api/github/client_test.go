package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_SetsRequiredHeaders drives the transport through GetPaginated —
// the only entry point production uses — so the header contract is asserted
// against the request the CLI actually sends.
func TestClient_SetsRequiredHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify required GitHub headers.
		if r.Header.Get("Authorization") != "Bearer gh-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Errorf("api version = %q", r.Header.Get("X-GitHub-Api-Version"))
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		_ = json.NewEncoder(w).Encode([]Assignment{{ID: 1, Slug: "lab-1", Title: "Lab 1"}})
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	var assignments []Assignment
	if err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &assignments); err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 || assignments[0].Slug != "lab-1" {
		t.Errorf("got %+v", assignments)
	}
}

func TestClient_GetPaginated(t *testing.T) {
	t.Parallel()
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		items := []Assignment{{ID: page, Slug: fmt.Sprintf("lab-%d", page), Title: fmt.Sprintf("Lab %d", page)}}
		if page < 3 {
			nextURL := fmt.Sprintf("http://%s%s?page=%d&per_page=1", r.Host, r.URL.Path, page+1)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	var assignments []Assignment
	if err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &assignments); err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 3 {
		t.Fatalf("len = %d, want 3", len(assignments))
	}
}

func TestClient_GetPaginated_ErrorRecovery(t *testing.T) {
	t.Parallel()
	// A mid-pagination failure must error out cleanly and leave the client
	// usable for the next request. First page succeeds, second returns 500.
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		if page == 1 {
			nextURL := fmt.Sprintf("http://%s%s?page=2", r.Host, r.URL.Path)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
			_ = json.NewEncoder(w).Encode([]Assignment{{ID: 1, Slug: "lab-1"}})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"server error"}`))
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	var assignments []Assignment
	err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &assignments)
	if err == nil {
		t.Fatal("expected error for 500 on second page")
	}

	// A follow-up request on the same client must succeed — the failed
	// pagination must not leave any state behind.
	page = 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Assignment{{ID: 2, Slug: "lab-2"}})
	}))
	defer srv2.Close()
	c.BaseURL = srv2.URL

	var result []Assignment
	if err := c.GetPaginated(context.Background(), "/test", nil, &result); err != nil {
		t.Fatalf("follow-up request failed (semaphore leak?): %v", err)
	}
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

func TestClient_NonOKStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	defer srv.Close()

	c := NewClient("bad-token")
	c.BaseURL = srv.URL

	var result []Assignment
	err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &result)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	// Assert the error came from the status check, not from decoding. The
	// body here is a JSON object and the decoder wants an array, so a bare
	// err != nil passes even with the status check removed entirely — the
	// test would keep guarding nothing.
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should name the status code, got: %v", err)
	}
	if !strings.Contains(err.Error(), "bad credentials") {
		t.Errorf("error should carry the response body, got: %v", err)
	}
}

func TestClient_GetPaginated_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Assignment{})
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	var assignments []Assignment
	if err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &assignments); err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 0 {
		t.Errorf("len = %d, want 0", len(assignments))
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Assignment{{ID: 1}})
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var assignments []Assignment
	err := c.GetPaginated(ctx, "/test", nil, &assignments)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
