// Package api wraps the generated Zotero client (internal/zot/client) with
// the cross-cutting concerns every caller needs: API-key injection, polite
// rate-limit backoff (Backoff / Retry-After headers, 5xx exponential backoff),
// and optimistic-concurrency retry on 412 Precondition Failed.
//
// Callers use typed helpers here (ItemCreate, ItemUpdate, CurrentKey, etc.)
// instead of touching the generated client directly.
package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/client"
)

// DefaultBaseURL is the Zotero Web API production endpoint.
const DefaultBaseURL = "https://api.zotero.org"

// Client wraps the generated Zotero client with auth, backoff, and retry.
type Client struct {
	Gen    *client.ClientWithResponses
	UserID client.UserID

	baseURL  string
	httpDoer HTTPDoer
	maxRetry int
	now      func() time.Time
	sleep    func(time.Duration)
}

// HTTPDoer matches the generated client's HttpRequestDoer interface so we
// can inject fakes in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Option customizes a Client at construction time.
type Option func(*Client)

// WithBaseURL overrides the API base URL (for tests against httptest servers).
func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

// WithHTTPClient injects a custom http.Client (for tests and request capture).
func WithHTTPClient(d HTTPDoer) Option { return func(c *Client) { c.httpDoer = d } }

// WithMaxRetries caps retries for 412/429/5xx. Defaults to 3.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetry = n } }

// WithClock overrides time.Now and time.Sleep (tests only).
func WithClock(now func() time.Time, sleep func(time.Duration)) Option {
	return func(c *Client) {
		c.now = now
		c.sleep = sleep
	}
}

// New constructs a Client from a Config. If cfg is nil, it is loaded from
// disk via zot.RequireConfig.
func New(cfg *zot.Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		var err error
		cfg, err = zot.RequireConfig()
		if err != nil {
			return nil, err
		}
	}

	uid, err := strconv.Atoi(cfg.LibraryID)
	if err != nil {
		return nil, fmt.Errorf("invalid library ID %q: %w", cfg.LibraryID, err)
	}

	c := &Client{
		UserID:   client.UserID(uid),
		baseURL:  DefaultBaseURL,
		maxRetry: 3,
		now:      time.Now,
		sleep:    time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.httpDoer == nil {
		c.httpDoer = &http.Client{Timeout: 30 * time.Second}
	}

	authFn := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Zotero-API-Key", cfg.APIKey)
		req.Header.Set("Zotero-API-Version", "3")
		return nil
	}

	gen, err := client.NewClientWithResponses(
		c.baseURL,
		client.WithHTTPClient(&retryDoer{inner: c.httpDoer, client: c}),
		client.WithRequestEditorFn(authFn),
	)
	if err != nil {
		return nil, fmt.Errorf("build zotero client: %w", err)
	}
	c.Gen = gen
	return c, nil
}
