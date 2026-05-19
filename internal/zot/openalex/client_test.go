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

// TestClient_Get_apiKeyTravelsInHeaderNotURL guards the credential leak fix.
// `api_key=` in the query string ends up in `*url.Error` strings on
// transport-level failures (DNS, TLS) and gets propagated to callers via
// fmt.Errorf("openalex: %w", err) — and from there into any TUI surface,
// --json error output, or agent telemetry that logs the error. Moving the
// key to an Authorization header keeps it out of those paths.
//
// `mailto=` stays in the URL: it's OpenAlex's polite-pool convention with no
// header equivalent, and the email is by-design public-ish.
func TestClient_Get_apiKeyTravelsInHeaderNotURL(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api_key"); got != "" {
			t.Errorf("api_key leaked into URL query: %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer secret")
		}
		if got := r.URL.Query().Get("mailto"); got != "me@example.com" {
			t.Errorf("mailto = %q, want me@example.com (polite-pool param stays in URL)", got)
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

// TestClient_Get_noAuthHeaderWithoutKey verifies that when the user hasn't
// configured an API key (the common case — OpenAlex works anonymously), we
// don't send an empty Authorization header.
func TestClient_Get_noAuthHeaderWithoutKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, present := r.Header["Authorization"]; present {
			t.Errorf("Authorization header sent without a configured key: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "W1"})
	}))
	defer srv.Close()

	c := NewClient("me@example.com", "")
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

func TestClient_Get_capsAbsurdRetryAfter(t *testing.T) {
	t.Parallel()
	// OpenAlex (or any upstream) sending a huge Retry-After must NOT park us
	// for that long — production hang on 2026-05-09 traced to a server
	// returning a multi-thousand-second Retry-After mid-batch, freezing the
	// entire scan with no progress. Cap to MaxRetryDelay.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.Header().Set("Retry-After", "7200") // 2 hours
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(Work{ID: "W1"})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	c.MaxRetryDelay = 50 * time.Millisecond

	start := time.Now()
	if err := c.Get(context.Background(), "/works/W1", nil, new(Work)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("retry slept %v, expected ≤ MaxRetryDelay (50ms) — Retry-After header not capped", elapsed)
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
