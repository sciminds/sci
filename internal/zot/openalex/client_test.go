package openalex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_Get(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/W1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ua := r.Header.Get("User-Agent"); !strings.HasPrefix(ua, "sci-zot") {
			t.Errorf("User-Agent = %q", ua)
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "https://openalex.org/W1"})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL

	var got Work
	if err := c.Get(context.Background(), "/works/W1", nil, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.ID, "/W1") {
		t.Errorf("id = %q", got.ID)
	}
}

func TestClient_Get_injectsAuthQueryParams(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("mailto"); got != "me@example.com" {
			t.Errorf("mailto = %q", got)
		}
		if got := r.URL.Query().Get("api_key"); got != "secret" {
			t.Errorf("api_key = %q", got)
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "W1"})
	}))
	defer srv.Close()

	c := NewClient("me@example.com", "secret")
	c.BaseURL = srv.URL
	if err := c.Get(context.Background(), "/works/W1", nil, new(Work)); err != nil {
		t.Fatal(err)
	}
}

func TestClient_Get_preservesCallerParams(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("search"); got != "attention is all you need" {
			t.Errorf("search = %q", got)
		}
		if got := r.URL.Query().Get("mailto"); got != "me@example.com" {
			t.Errorf("mailto = %q", got)
		}
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	defer srv.Close()

	c := NewClient("me@example.com", "")
	c.BaseURL = srv.URL
	params := map[string][]string{"search": {"attention is all you need"}}
	if err := c.Get(context.Background(), "/works", params, new(struct{})); err != nil {
		t.Fatal(err)
	}
}

func TestClient_Get_retryOn429(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "W1"})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	c.RetryBaseDelay = time.Millisecond

	if err := c.Get(context.Background(), "/works/W1", nil, new(Work)); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestClient_Get_retryOn5xx(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "W1"})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	c.RetryBaseDelay = time.Millisecond
	if err := c.Get(context.Background(), "/works/W1", nil, new(Work)); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestClient_Get_surfaces4xxError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	err := c.Get(context.Background(), "/works/Wnope", nil, new(Work))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}
