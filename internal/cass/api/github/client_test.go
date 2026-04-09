package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClient_Get(t *testing.T) {
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
	if err := c.Get(context.Background(), "/classrooms/1/assignments", nil, &assignments); err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 || assignments[0].Slug != "lab-1" {
		t.Errorf("got %+v", assignments)
	}
}

func TestClient_GetPaginated(t *testing.T) {
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

func TestClient_GetPaginated_SemaphoreSafety(t *testing.T) {
	// Verifies semaphore is properly released even when requests error mid-pagination.
	// First page succeeds, second page returns 500.
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
	c.MaxConcurrent = 2

	var assignments []Assignment
	err := c.GetPaginated(context.Background(), "/classrooms/1/assignments", nil, &assignments)
	if err == nil {
		t.Fatal("expected error for 500 on second page")
	}

	// After the error, the semaphore must be fully drained (not leaked).
	// Verify by doing another successful request — if semaphore leaked, this would deadlock.
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
	}))
	defer srv.Close()

	c := NewClient("bad-token")
	c.BaseURL = srv.URL

	var result []Assignment
	err := c.Get(context.Background(), "/classrooms/1/assignments", nil, &result)
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestClient_GetPaginated_EmptyResponse(t *testing.T) {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]Assignment{{ID: 1}})
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var assignments []Assignment
	err := c.Get(ctx, "/test", nil, &assignments)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClient_GetConcurrent(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		_ = json.NewEncoder(w).Encode([]Commit{{SHA: "abc123"}})
	}))
	defer srv.Close()

	c := NewClient("gh-token")
	c.BaseURL = srv.URL

	paths := []string{
		"/repos/org/repo1/commits",
		"/repos/org/repo2/commits",
		"/repos/org/repo3/commits",
	}

	results, err := GetConcurrent[[]Commit](context.Background(), c, paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("len = %d, want 3", len(results))
	}
	if callCount.Load() != 3 {
		t.Errorf("call count = %d, want 3", callCount.Load())
	}
}
