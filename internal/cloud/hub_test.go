package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func stubHubRetry(t *testing.T, now func() time.Time, sleep func(context.Context, time.Duration) error) {
	t.Helper()
	originalNow := hubRetryNow
	originalSleep := hubRetrySleep
	if now != nil {
		hubRetryNow = now
	}
	if sleep != nil {
		hubRetrySleep = sleep
	}
	t.Cleanup(func() {
		hubRetryNow = originalNow
		hubRetrySleep = originalSleep
	})
}

func skipHubRetrySleep(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

// hubStub stands up a fake Hub, points HF_ENDPOINT at it, and records the
// paths (with query) each request asked for.
func hubStub(t *testing.T, h http.HandlerFunc) *[]string {
	t.Helper()
	paths := new([]string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.RequestURI())
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HF_ENDPOINT", srv.URL)
	return paths
}

func TestListBucketTree_RequestShape(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	var gotAuth string
	paths := hubStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "private", "alice/my data/"); err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(*paths) != 1 {
		t.Fatalf("made %d requests, want 1", len(*paths))
	}
	got := (*paths)[0]
	// The prefix is one path segment, so a space in it must be escaped rather
	// than splitting the URL, and the trailing slash must not double up.
	if want := "/api/buckets/sciminds/private/tree/alice%2Fmy%20data?recursive=true"; got != want {
		t.Errorf("request = %q, want %q", got, want)
	}
	if want := "Bearer hf_abc"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestListBucketTree_NoPrefixListsWholeBucket(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	paths := hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "public", ""); err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if want := "/api/buckets/sciminds/public/tree?recursive=true"; (*paths)[0] != want {
		t.Errorf("request = %q, want %q", (*paths)[0], want)
	}
}

func TestListBucketTree_RetriesRateLimit(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	requests := 0
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"type":"file","path":"alice/data.csv"}]`))
	})

	files, err := listBucketTree(context.Background(), "sciminds", "private", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v, want retry to succeed", err)
	}
	if requests != 2 {
		t.Errorf("requests = %d, want 2", requests)
	}
	if len(files) != 1 || files[0].Path != "alice/data.csv" {
		t.Errorf("files = %+v, want the successful retry response", files)
	}
}

func TestHubGet_RetriesRetryableStatuses(t *testing.T) {
	for _, status := range []int{
		http.StatusRequestTimeout,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			setHFToken(t, "hf_abc", nil)
			stubHubRetry(t, nil, skipHubRetrySleep)
			requests := 0
			paths := hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				if requests == 1 {
					w.WriteHeader(status)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			})

			resp, err := hubGet(context.Background(), hfEndpoint()+"/retry")
			if err != nil {
				t.Fatalf("hubGet() error = %v, want retry to succeed", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || requests != 2 || len(*paths) != 2 {
				t.Errorf("status = %d, requests = %d, want 200 after 2 requests", resp.StatusCode, requests)
			}
		})
	}
}

func TestHubGet_DoesNotRetryOtherClientErrors(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			setHFToken(t, "hf_abc", nil)
			stubHubRetry(t, nil, func(context.Context, time.Duration) error {
				t.Fatal("non-retryable response attempted to sleep")
				return nil
			})
			requests := 0
			hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(status)
			})

			resp, err := hubGet(context.Background(), hfEndpoint()+"/rejected")
			if err != nil {
				t.Fatalf("hubGet() transport error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if requests != 1 {
				t.Errorf("requests = %d, want exactly 1 for %d", requests, status)
			}
		})
	}
}

func TestListBucketTree_RetriesTransportError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	var requests atomic.Int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack first request: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`[{"type":"file","path":"alice/data.csv"}]`))
	}))
	t.Cleanup(hub.Close)
	t.Setenv("HF_ENDPOINT", hub.URL)

	files, err := listBucketTree(context.Background(), "sciminds", "private", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v, want transport retry to succeed", err)
	}
	if requests.Load() != 2 || len(files) != 1 {
		t.Errorf("requests = %d, files = %+v, want success on request 2", requests.Load(), files)
	}
}

func TestListBucketTree_RetriesExhaustAndReturnLastError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	var delays []time.Duration
	stubHubRetry(t, nil, func(ctx context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return ctx.Err()
	})
	statuses := []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusGatewayTimeout,
		http.StatusServiceUnavailable,
	}
	requests := 0
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statuses[requests])
		requests++
	})

	_, err := listBucketTree(context.Background(), "sciminds", "private", "")
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("listBucketTree() error = %v, want the last 503 error", err)
	}
	if requests != hubRetryMaxAttempts {
		t.Errorf("requests = %d, want bounded maximum %d", requests, hubRetryMaxAttempts)
	}
	wantDelays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	if !slices.Equal(delays, wantDelays) {
		t.Errorf("delays = %v, want exponential backoff %v", delays, wantDelays)
	}
}

func TestHubGet_HonorsRetryAfter(t *testing.T) {
	fixedNow := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    int
		header    string
		wantDelay time.Duration
	}{
		{name: "delta seconds", status: http.StatusTooManyRequests, header: "3", wantDelay: 3 * time.Second},
		{name: "HTTP date", status: http.StatusServiceUnavailable, header: fixedNow.Add(4 * time.Second).Format(http.TimeFormat), wantDelay: 4 * time.Second},
		{name: "absurd delay is clamped", status: http.StatusTooManyRequests, header: "86400", wantDelay: hubRetryAfterCap},
		{name: "invalid header uses backoff", status: http.StatusServiceUnavailable, header: "eventually", wantDelay: hubRetryBaseDelay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHFToken(t, "hf_abc", nil)
			var delays []time.Duration
			stubHubRetry(t, func() time.Time { return fixedNow }, func(ctx context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return ctx.Err()
			})
			requests := 0
			hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				if requests == 1 {
					w.Header().Set("Retry-After", tt.header)
					w.WriteHeader(tt.status)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			})

			resp, err := hubGet(context.Background(), hfEndpoint()+"/retry-after")
			if err != nil {
				t.Fatalf("hubGet() error = %v", err)
			}
			_ = resp.Body.Close()
			if len(delays) != 1 || delays[0] != tt.wantDelay {
				t.Errorf("delays = %v, want [%v]", delays, tt.wantDelay)
			}
		})
	}
}

func TestHubDo_CanceledWaitReturnsNoClosedResponse(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("try later"))
	}))
	t.Cleanup(hub.Close)
	ctx, cancel := context.WithCancel(context.Background())
	stubHubRetry(t, nil, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hub.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := hubDo(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("hubDo() error = %v, want context.Canceled", err)
	}
	if resp != nil {
		_, readErr := resp.Body.Read(make([]byte, 1))
		t.Errorf("hubDo() response = %s, want nil; body reread error = %v", resp.Status, readErr)
	}
}

func TestHubDo_UnaffordableWaitReturnsOpenLastResponse(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	stubHubRetry(t, func() time.Time { return deadline.Add(-time.Second) }, func(context.Context, time.Duration) error {
		t.Fatal("unaffordable retry attempted to sleep")
		return nil
	})
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("try later"))
	}))
	t.Cleanup(hub.Close)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hub.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := hubDo(ctx, req)
	if err != nil {
		t.Fatalf("hubDo() error = %v, want the last real response", err)
	}
	if resp == nil {
		t.Fatal("hubDo() response = nil, want the last real response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("hubDo() status = %s, want 503 Service Unavailable", resp.Status)
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil || string(body) != "try later" {
		t.Errorf("hubDo() body = %q, error = %v, want an open %q body", body, readErr, "try later")
	}
}

func TestListBucketTree_DeadlineShorterThanBackoffReturnsLastError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	slept := false
	stubHubRetry(t, nil, func(context.Context, time.Duration) error {
		slept = true
		return nil
	})
	requests := 0
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithTimeout(context.Background(), hubRetryBaseDelay/2)
	defer cancel()

	_, err := listBucketTree(ctx, "sciminds", "private", "")
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("listBucketTree() error = %v, want the last 503 error", err)
	}
	if slept || requests != 1 {
		t.Errorf("slept = %v, requests = %d, want no sleep and exactly 1 request", slept, requests)
	}
}

func TestListBucketTree_CancelDuringBackoffReturnsPromptly(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	sleepStarted := make(chan struct{})
	stubHubRetry(t, nil, func(ctx context.Context, delay time.Duration) error {
		close(sleepStarted)
		return sleepHubRetry(ctx, delay)
	})
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := listBucketTree(ctx, "sciminds", "private", "")
		result <- err
	}()
	<-sleepStarted
	cancel()

	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("listBucketTree() error = %v, want context.Canceled", err)
		}
	case <-timer.C:
		t.Fatal("listBucketTree() did not return promptly after cancellation")
	}
}

func TestHubGet_DrainsRetriedResponseForConnectionReuse(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	requests := 0
	connections := make(map[string]struct{})
	retryBody := strings.Repeat("x", 256*1024)
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		connections[r.RemoteAddr] = struct{}{}
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(retryBody))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(hub.Close)
	t.Setenv("HF_ENDPOINT", hub.URL)
	originalClient := hubHTTPClient
	hubHTTPClient = hub.Client()
	t.Cleanup(func() { hubHTTPClient = originalClient })

	if _, err := listBucketTree(context.Background(), "sciminds", "private", ""); err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if requests != 2 || len(connections) != 1 {
		t.Errorf("requests = %d over %d connections, want 2 requests over 1 reused connection", requests, len(connections))
	}
}

func TestListBucketTree_FollowsPagination(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?cursor=p2>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"type":"file","path":"a.csv","size":1}]`))
		case "p2":
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?cursor=p3>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"type":"file","path":"b.csv","size":2}]`))
		default:
			_, _ = w.Write([]byte(`[{"type":"file","path":"c.csv","size":3}]`))
		}
	}))
	t.Cleanup(srv.Close)
	base = srv.URL
	t.Setenv("HF_ENDPOINT", srv.URL)

	files, err := listBucketTree(context.Background(), "o", "b", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3 (all pages concatenated)", len(files))
	}
	if files[0].Path != "a.csv" || files[2].Path != "c.csv" {
		t.Errorf("files = %+v, want page order preserved", files)
	}
}

func TestListBucketTree_DiscardsEarlierPagesWhenLaterPageFails(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	requests := 0
	var base string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?cursor=p2>; rel="next"`, base))
			_, _ = w.Write([]byte(`[{"type":"file","path":"first.csv"}]`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(hub.Close)
	base = hub.URL
	t.Setenv("HF_ENDPOINT", hub.URL)

	files, err := listBucketTree(context.Background(), "o", "b", "")
	if err == nil {
		t.Fatal("listBucketTree() error = nil, want the second-page failure")
	}
	if files != nil {
		t.Errorf("files = %+v, want nil rather than a partial listing", files)
	}
	if requests != 1+hubRetryMaxAttempts {
		t.Errorf("requests = %d, want first page plus %d attempts for page two", requests, hubRetryMaxAttempts)
	}
}

func TestListBucketTree_RejectsCrossOriginNextLinkBeforeSendingToken(t *testing.T) {
	setHFToken(t, "hf_secret", nil)
	attackerRequests := make(chan string, 1)
	attacker := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerRequests <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	attacker.Start()
	hub := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/steal>; rel="next"`, attacker.URL))
		_, _ = w.Write([]byte(`[]`))
	}))
	hub.Start()
	t.Setenv("HF_ENDPOINT", hub.URL)

	_, err := listBucketTree(context.Background(), "o", "b", "")
	if err == nil {
		t.Fatal("listBucketTree() error = nil, want a cross-origin pagination error")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Errorf("listBucketTree() error = %q, want it to name the cross-origin link", err)
	}
	select {
	case gotAuth := <-attackerRequests:
		t.Errorf("attacker server received Authorization = %q, want no request carrying the token", gotAuth)
	default:
	}
}

func TestListBucketTree_ResolvesRelativeNextLink(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	requests := 0
	hub := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("Link", `</api/buckets/o/b/tree?cursor=p2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"type":"file","path":"a.csv"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"type":"file","path":"b.csv"}]`))
	}))
	hub.Start()
	t.Setenv("HF_ENDPOINT", hub.URL)

	files, err := listBucketTree(context.Background(), "o", "b", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if requests != 2 || len(files) != 2 {
		t.Errorf("made %d requests and got %d files, want 2 requests and 2 files", requests, len(files))
	}
}

func TestListBucketTree_RejectsPaginationCycleImmediately(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	requests := 0
	var hubURL string
	hub := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Link", fmt.Sprintf(`<%s/api/buckets/o/b/tree?recursive=true>; rel="next"`, hubURL))
		_, _ = w.Write([]byte(`[]`))
	}))
	hub.Start()
	hubURL = hub.URL
	t.Setenv("HF_ENDPOINT", hub.URL)

	_, err := listBucketTree(context.Background(), "o", "b", "")
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("listBucketTree() error = %v, want a pagination cycle error", err)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1 before detecting the cycle", requests)
	}
}

func TestListBucketTree_PageCapReturnsTruncationError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	requests := 0
	var hubURL string
	hub := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		nextPage := requests + 1
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?recursive=true&page=%d>; rel="next"`, hubURL, r.URL.Path, nextPage))
		_, _ = w.Write([]byte(`[]`))
	}))
	hub.Start()
	hubURL = hub.URL
	t.Setenv("HF_ENDPOINT", hub.URL)

	_, err := listBucketTree(context.Background(), "o", "b", "")
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("listBucketTree() error = %v, want a truncation error", err)
	}
	if requests != hubMaxPages {
		t.Errorf("made %d requests, want the %d-page cap", requests, hubMaxPages)
	}
}

func TestHubHTTPClient_BoundsStalledRequestAndIsInjectable(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	if hubHTTPClient.Timeout != hubHTTPTimeout || hubHTTPClient.Timeout <= 0 {
		t.Fatalf("hubHTTPClient.Timeout = %v, want the bounded package default %v", hubHTTPClient.Timeout, hubHTTPTimeout)
	}
	hub := httptest.NewTestServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	client := hub.Client()
	client.Timeout = 20 * time.Millisecond
	originalClient := hubHTTPClient
	hubHTTPClient = client
	t.Cleanup(func() { hubHTTPClient = originalClient })

	_, err := hubGet(context.Background(), hub.URL+"/stall")
	if err == nil {
		t.Fatal("hubGet() error = nil, want the client timeout to stop a stalled response")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hubGet() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestListBucketTree_MissingPrefixIsEmptyNotAnError(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Entry not found"}`))
	})

	files, err := listBucketTree(context.Background(), "sciminds", "private", "ghost/")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v, want nil for an absent prefix", err)
	}
	if len(files) != 0 {
		t.Errorf("files = %+v, want empty", files)
	}
}

func TestListBucketTree_ServerErrorSurfaces(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	stubHubRetry(t, nil, skipHubRetrySleep)
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := listBucketTree(context.Background(), "sciminds", "private", ""); err == nil {
		t.Fatal("listBucketTree() error = nil, want the 500 surfaced")
	}
}

func TestListBucketTree_ParsesAPIFieldNames(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	// The Hub answers in camelCase; the `hf buckets ls --json` output this
	// replaced used snake_case, so the tags must track the API.
	hubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"type":"file","path":"alice/a.csv","size":12,` +
			`"mtime":"2026-05-01T00:00:00.000Z","uploadedAt":"2026-05-02T00:00:00.000Z"}]`))
	})

	files, err := listBucketTree(context.Background(), "sciminds", "private", "")
	if err != nil {
		t.Fatalf("listBucketTree() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]
	if f.Type != "file" || f.Path != "alice/a.csv" || f.Size != 12 {
		t.Errorf("file = %+v", f)
	}
	if !strings.HasPrefix(f.MTime, "2026-05-01") {
		t.Errorf("MTime = %q, want the mtime field decoded", f.MTime)
	}
	if !strings.HasPrefix(f.UploadedAt, "2026-05-02") {
		t.Errorf("UploadedAt = %q, want the camelCase uploadedAt field decoded", f.UploadedAt)
	}
}
