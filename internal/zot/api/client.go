// Package api wraps the generated Zotero client (internal/zot/client) with
// the cross-cutting concerns every caller needs: API-key injection, polite
// rate-limit backoff (Backoff / Retry-After headers, 5xx exponential backoff),
// and optimistic-concurrency retry on 412 Precondition Failed.
//
// Callers use typed helpers here (ItemCreate, ItemUpdate, CurrentKey, etc.)
// instead of touching the generated client directly. Each library-scoped
// helper dispatches to the user- or group-variant generated method based
// on the Client's Lib scope (see WithLibrary).
package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/client"
)

// DefaultBaseURL is the Zotero Web API production endpoint.
const DefaultBaseURL = "https://api.zotero.org"

// Client wraps the generated Zotero client with auth, backoff, and retry.
//
// Lib identifies the target library (personal or shared) and drives per-op
// dispatch between `c.Gen.{Op}WithResponse(ctx, UserID, ...)` and
// `c.Gen.{Op}GroupWithResponse(ctx, GroupID, ...)`. Set it via WithLibrary —
// New errors if it is not provided.
type Client struct {
	Gen    *client.ClientWithResponses
	UserID client.UserID
	Lib    zot.LibraryRef

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

// WithLibrary pins the client to a specific library (personal or shared).
// When unset, New defaults to LibPersonal built from cfg.UserID.
func WithLibrary(ref zot.LibraryRef) Option {
	return func(c *Client) { c.Lib = ref }
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

	uid, err := strconv.Atoi(cfg.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID %q: %w", cfg.UserID, err)
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
	if c.Lib.Scope == "" {
		return nil, fmt.Errorf("api.New: WithLibrary is required (scope must be set)")
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

// GroupID extracts the numeric group ID from Lib.APIPath. Panics if called
// on a non-shared-scope Client — every caller is inside a shared-scope
// branch of a dispatch switch, so the panic guards against misuse rather
// than handles it.
func (c *Client) GroupID() client.GroupID {
	if c.Lib.Scope != zot.LibShared {
		panic(fmt.Sprintf("GroupID called with scope %q (want shared)", c.Lib.Scope))
	}
	rest := strings.TrimPrefix(c.Lib.APIPath, "groups/")
	id, err := strconv.Atoi(rest)
	if err != nil {
		panic(fmt.Sprintf("invalid shared library APIPath %q: %v", c.Lib.APIPath, err))
	}
	return client.GroupID(id)
}

// isShared reports whether the client is dispatching to /groups/... endpoints.
func (c *Client) isShared() bool { return c.Lib.Scope == zot.LibShared }
