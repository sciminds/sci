package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync/atomic"
	"testing"
	"time"
)

// countingHandler answers fail(n) for the first failures calls, then the
// key-info payload. n is 1-based.
func countingHandler(failures int32, fail func(w http.ResponseWriter, n int32)) (http.Handler, *atomic.Int32) {
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n <= failures {
			fail(w, n)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyInfoJSON))
	})
	return h, &calls
}

func TestRetry_429HonoursRetryAfter(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(2, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, log := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
	if got := log.total(); got != 4*time.Second {
		t.Errorf("slept %v, want 4s (2 × Retry-After 2s)", got)
	}
}

func TestRetry_429WithoutRetryAfterUsesBackoff(t *testing.T) {
	t.Parallel()
	h, _ := countingHandler(1, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, log := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := log.total(); got != 200*time.Millisecond {
		t.Errorf("slept %v, want 200ms", got)
	}
}

func TestRetry_5xxExponentialBackoff(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(2, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c, log := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
	want := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	if len(log.sleeps) != 2 || log.sleeps[0] != want[0] || log.sleeps[1] != want[1] {
		t.Errorf("sleeps = %v, want %v", log.sleeps, want)
	}
}

func TestRetry_BackoffIsCappedAtTenSeconds(t *testing.T) {
	t.Parallel()
	if got := backoffDelay(10); got != 10*time.Second {
		t.Errorf("backoffDelay(10) = %v, want 10s", got)
	}
	if got := backoffDelay(0); got != 200*time.Millisecond {
		t.Errorf("backoffDelay(0) = %v, want 200ms", got)
	}
}

func TestRetry_GivesUpAfterMaxRetries(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(100, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, _ := newTestClient(t, h, WithMaxRetries(2))

	_, err := c.CurrentKey(context.Background())
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok || herr.Status != http.StatusTooManyRequests {
		t.Fatalf("error = %v, want *HTTPError with status 429", err)
	}
	// maxRetries=2 → 3 attempts in total.
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestRetry_5xxExhaustedSurfacesStatusAndBody(t *testing.T) {
	t.Parallel()
	h, _ := countingHandler(100, func(w http.ResponseWriter, _ int32) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	})
	c, _ := newTestClient(t, h, WithMaxRetries(1))

	_, err := c.CurrentKey(context.Background())
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok {
		t.Fatalf("error = %v, want *HTTPError", err)
	}
	if herr.Status != http.StatusInternalServerError || herr.Body != "upstream exploded" {
		t.Errorf("HTTPError = %+v, want 500 with the server's body", *herr)
	}
}

func TestRetry_BackoffHeaderDelaysNextRequest(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Backoff", "3")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyInfoJSON))
	})
	c, log := newTestClient(t, h)

	ctx := context.Background()
	if _, err := c.CurrentKey(ctx); err != nil {
		t.Fatal(err)
	}
	if len(log.sleeps) != 0 {
		t.Fatalf("slept %v after the first (successful) response, want the delay before the NEXT request", log.sleeps)
	}
	if _, err := c.CurrentKey(ctx); err != nil {
		t.Fatal(err)
	}
	if len(log.sleeps) != 1 || log.sleeps[0] != 3*time.Second {
		t.Errorf("sleeps = %v, want [3s] before the second request", log.sleeps)
	}
	if _, err := c.CurrentKey(ctx); err != nil {
		t.Fatal(err)
	}
	if len(log.sleeps) != 1 {
		t.Errorf("sleeps = %v, want the Backoff honoured exactly once", log.sleeps)
	}
}

// flakyDoer fails the first failures calls with a network error, then
// delegates to inner. It records each attempt's body and write token so a
// test can check what a retry re-sent.
type flakyDoer struct {
	inner       HTTPDoer
	failures    int
	calls       int
	bodies      []string
	writeTokens []string
}

func (d *flakyDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	d.writeTokens = append(d.writeTokens, req.Header.Get("Zotero-Write-Token"))
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		d.bodies = append(d.bodies, string(b))
		req.Body = io.NopCloser(bytes.NewReader(b)) // hand the transport an unread copy
	}
	if d.calls <= d.failures {
		return nil, errors.New("connection reset")
	}
	return d.inner.Do(req)
}

func TestRetry_NetworkErrorRetriesWithBackoff(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, log := newTestClient(t, f)
	flaky := &flakyDoer{inner: c.http, failures: 2}
	c.http = flaky

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if flaky.calls != 3 {
		t.Errorf("calls = %d, want 3", flaky.calls)
	}
	if got := log.total(); got != 600*time.Millisecond {
		t.Errorf("slept %v, want 600ms (200ms + 400ms)", got)
	}
}

func TestRetry_NetworkErrorExhaustedReturnsIt(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f, WithMaxRetries(1))
	flaky := &flakyDoer{inner: c.http, failures: 100}
	c.http = flaky

	_, err := c.CurrentKey(context.Background())
	if err == nil || err.Error() == "" {
		t.Fatalf("error = %v, want the network error", err)
	}
	if flaky.calls != 2 {
		t.Errorf("calls = %d, want 2", flaky.calls)
	}
}

func TestRetry_ResendsBufferedBody(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)
	flaky := &flakyDoer{inner: c.http, failures: 1}
	c.http = flaky

	if _, err := c.CreateItem(context.Background(), map[string]any{"itemType": "book", "title": "T"}); err != nil {
		t.Fatal(err)
	}
	if len(flaky.bodies) != 2 || flaky.bodies[0] != flaky.bodies[1] || flaky.bodies[1] == "" {
		t.Errorf("bodies = %q, want the same non-empty body sent twice", flaky.bodies)
	}
}

// writeTokenRe is Zotero's Zotero-Write-Token shape: exactly 32 characters;
// the client sends lowercase hex.
var writeTokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestCreateItem_WriteTokenIsStableAcrossRetries(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)
	flaky := &flakyDoer{inner: c.http, failures: 1}
	c.http = flaky

	if _, err := c.CreateItem(context.Background(), map[string]any{"itemType": "book"}); err != nil {
		t.Fatal(err)
	}
	if len(flaky.writeTokens) != 2 {
		t.Fatalf("attempts = %d, want 2", len(flaky.writeTokens))
	}
	if !writeTokenRe.MatchString(flaky.writeTokens[0]) {
		t.Errorf("Zotero-Write-Token = %q, want 32 hex characters", flaky.writeTokens[0])
	}
	if flaky.writeTokens[0] != flaky.writeTokens[1] {
		t.Errorf("tokens differ across retries (%q vs %q); a resend must be recognisable as the same write",
			flaky.writeTokens[0], flaky.writeTokens[1])
	}
}

func TestCreate_WriteTokenIsFreshPerCall(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	c, _ := newTestClient(t, f)
	ctx := context.Background()

	if _, err := c.CreateItem(ctx, map[string]any{"itemType": "book"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCollection(ctx, "Papers", ""); err != nil {
		t.Fatal(err)
	}
	seen := f.seen()
	first, second := seen[0].Header.Get("Zotero-Write-Token"), seen[1].Header.Get("Zotero-Write-Token")
	if !writeTokenRe.MatchString(first) || !writeTokenRe.MatchString(second) {
		t.Errorf("tokens = %q, %q; want 32 hex characters on both creates", first, second)
	}
	if first == second {
		t.Errorf("two creates carried the same write token %q; Zotero would drop the second", first)
	}
}

func TestReads_CarryNoWriteToken(t *testing.T) {
	t.Parallel()
	f := newFakeZotero(t)
	f.seedItem("ABCD1234", 7, map[string]any{"itemType": "book"})
	c, _ := newTestClient(t, f)

	if _, err := c.GetItem(context.Background(), "ABCD1234"); err != nil {
		t.Fatal(err)
	}
	if tok := f.seen()[0].Header.Get("Zotero-Write-Token"); tok != "" {
		t.Errorf("GET carried Zotero-Write-Token %q, want none", tok)
	}
}

func TestRetry_429WithBackoffSleepsOnceTheLarger(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(1, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "2")
		w.Header().Set("Backoff", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, log := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
	if len(log.sleeps) != 1 || log.sleeps[0] != 5*time.Second {
		t.Errorf("sleeps = %v, want exactly one wait of 5s (the larger of Retry-After and Backoff)", log.sleeps)
	}
	// The Backoff was consumed by the retry; it must not fire again before
	// the next request.
	if _, err := c.CurrentKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(log.sleeps) != 1 {
		t.Errorf("sleeps = %v, want no second wait", log.sleeps)
	}
}

func TestRetry_RetryAfterAboveCapReturns429Unslept(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(1, func(w http.ResponseWriter, _ int32) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c, log := newTestClient(t, h)

	_, err := c.CurrentKey(context.Background())
	herr, ok := errors.AsType[*HTTPError](err)
	if !ok || herr.Status != http.StatusTooManyRequests {
		t.Fatalf("error = %v, want *HTTPError 429 handed back untouched", err)
	}
	if calls.Load() != 1 || len(log.sleeps) != 0 {
		t.Errorf("calls = %d, sleeps = %v; want one call and no sleep for a wait above %v", calls.Load(), log.sleeps, maxServerWait)
	}
}

func TestRetry_BackoffAboveCapIsClamped(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Backoff", "3600")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(keyInfoJSON))
	})
	c, log := newTestClient(t, h)
	ctx := context.Background()

	if _, err := c.CurrentKey(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CurrentKey(ctx); err != nil {
		t.Fatal(err)
	}
	if len(log.sleeps) != 1 || log.sleeps[0] != maxServerWait {
		t.Errorf("sleeps = %v, want [%v] (Backoff clamped to the cap)", log.sleeps, maxServerWait)
	}
}

func TestRetry_CancellationMidWaitReturnsCtxErr(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(100, func(w http.ResponseWriter, _ int32) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	// A sleep that never returns on its own: the only way out is the
	// context being cancelled while the client is waiting.
	stuckSleep := func(time.Duration) {
		cancel()
		<-release
	}
	c, _ := newTestClient(t, h, WithSleep(stuckSleep))

	_, err := c.CurrentKey(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled from the interrupted wait", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no request after cancellation)", calls.Load())
	}
}

func TestRetry_4xxIsNotRetried(t *testing.T) {
	t.Parallel()
	h, calls := countingHandler(100, func(w http.ResponseWriter, _ int32) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	})
	c, log := newTestClient(t, h)

	if _, err := c.CurrentKey(context.Background()); err == nil {
		t.Fatal("error = nil, want *HTTPError")
	}
	if calls.Load() != 1 || len(log.sleeps) != 0 {
		t.Errorf("calls = %d, sleeps = %v; want one call and no sleeps", calls.Load(), log.sleeps)
	}
}
