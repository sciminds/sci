package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/zot"
)

// fakeClock records sleeps without actually pausing.
type fakeClock struct {
	slept time.Duration
}

func (f *fakeClock) now() time.Time        { return time.Unix(0, 0) }
func (f *fakeClock) sleep(d time.Duration) { f.slept += d }

func testCfg() *zot.Config {
	return &zot.Config{APIKey: "test-key", LibraryID: "42"}
}

func newTestClient(t *testing.T, handler http.Handler, opts ...Option) (*Client, *fakeClock) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	fc := &fakeClock{}
	all := append([]Option{
		WithBaseURL(srv.URL),
		WithClock(fc.now, fc.sleep),
	}, opts...)
	c, err := New(testCfg(), all...)
	if err != nil {
		t.Fatal(err)
	}
	return c, fc
}

func TestCurrentKey_AuthHeaderInjected(t *testing.T) {
	var gotKey, gotVersion string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Zotero-API-Key")
		gotVersion = r.Header.Get("Zotero-API-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"test-key","userID":42,"username":"alice","access":{}}`))
	})
	c, _ := newTestClient(t, h)

	info, err := c.CurrentKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "test-key" {
		t.Errorf("Zotero-API-Key = %q, want test-key", gotKey)
	}
	if gotVersion != "3" {
		t.Errorf("Zotero-API-Version = %q, want 3", gotVersion)
	}
	if info.UserID != 42 || info.Username != "alice" {
		t.Errorf("unexpected key info: %+v", info)
	}
}

func TestRetry_On429HonorsRetryAfter(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"k","userID":1,"username":"u","access":{}}`))
	})
	c, fc := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
	// 2 failures × 2s Retry-After = 4s total slept.
	if fc.slept != 4*time.Second {
		t.Errorf("slept = %v, want 4s", fc.slept)
	}
}

func TestRetry_On5xxExponentialBackoff(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"k","userID":1,"username":"u","access":{}}`))
	})
	c, fc := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	// First retry: 200ms, second retry: 400ms → 600ms total.
	if fc.slept != 600*time.Millisecond {
		t.Errorf("slept = %v, want 600ms", fc.slept)
	}
}

func TestRetry_GivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, _ := newTestClient(t, h, WithMaxRetries(2))

	_, err := c.CurrentKey(context.Background())
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// maxRetry=2 → 3 total attempts (attempt 0, 1, 2).
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestBackoffHeader_DelaysNextRequest(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		// Every successful response carries Backoff: 5 (Zotero's polite hint).
		w.Header().Set("Backoff", "5")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"k","userID":1,"username":"u","access":{}}`))
	})
	c, fc := newTestClient(t, h)

	// First call: no prior wait, records Backoff=5 for next call.
	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.slept != 0 {
		t.Errorf("first call slept %v, want 0", fc.slept)
	}
	// Second call: honors the 5s hint before sending.
	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.slept != 5*time.Second {
		t.Errorf("second call slept %v, want 5s", fc.slept)
	}
}

func TestParseSeconds(t *testing.T) {
	tests := map[string]time.Duration{
		"":    0,
		"0":   0,
		"5":   5 * time.Second,
		"abc": 0,
		"-3":  0,
	}
	for in, want := range tests {
		if got := parseSeconds(in); got != want {
			t.Errorf("parseSeconds(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBackoffDelay_Cap(t *testing.T) {
	// 200ms, 400ms, 800ms, ..., capped at 10s.
	for attempt, want := range map[int]time.Duration{
		0:  200 * time.Millisecond,
		1:  400 * time.Millisecond,
		2:  800 * time.Millisecond,
		10: 10 * time.Second, // capped
	} {
		if got := backoffDelay(attempt); got != want {
			t.Errorf("backoffDelay(%d) = %v, want %v", attempt, got, want)
		}
	}
}
