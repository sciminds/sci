package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/sciminds/sci/internal/zot"
)

// DefaultBaseURL is the public Zotero Web API endpoint.
const DefaultBaseURL = "https://api.zotero.org"

// apiVersion is the Zotero-API-Version header value every request carries.
const apiVersion = "3"

// Credentials is what a write needs: the user's API key, their numeric user
// ID, and (only for the shared scope) the numeric Zotero group ID.
type Credentials struct {
	APIKey  string
	UserID  string
	GroupID string
}

// HTTPDoer is the transport a [Client] sends through — *http.Client
// satisfies it, and tests substitute their own.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Option customises a [Client] at construction.
type Option func(*Client)

// WithBaseURL points the client at a different API root (a test server).
// A trailing slash is dropped so paths can always start with "/".
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// WithHTTPClient replaces the transport. The default is an *http.Client
// with a 30s timeout.
func WithHTTPClient(d HTTPDoer) Option {
	return func(c *Client) { c.http = d }
}

// WithMaxRetries sets how many times a request is retried after a 429,
// a 5xx or a network error — so n+1 attempts in total. The default is 3.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithSleep replaces the function the retry layer pauses with. Tests inject
// a recorder so a retry schedule can be asserted without waiting through it.
func WithSleep(fn func(time.Duration)) Option {
	return func(c *Client) { c.sleep = fn }
}

// Client talks to one Zotero library. Construct it with [New]; the zero
// value is not usable.
type Client struct {
	creds      Credentials
	lib        zot.LibraryRef
	baseURL    string
	http       HTTPDoer
	maxRetries int
	sleep      func(time.Duration)

	// mu guards the two pieces of state a request can update: the Backoff
	// delay carried into the next request, and the schema cache.
	mu          sync.Mutex
	pendingWait time.Duration
	fieldsCache map[string][]string
}

// New validates the credentials against the target library and returns a
// client bound to it. A write targets exactly one library, so lib.Scope
// must be [zot.LibPersonal] or [zot.LibShared]; the shared scope
// additionally needs a numeric GroupID.
func New(creds Credentials, lib zot.LibraryRef, opts ...Option) (*Client, error) {
	if creds.APIKey == "" {
		return nil, fmt.Errorf("zotero api: API key is required")
	}
	if !isNumeric(creds.UserID) {
		return nil, fmt.Errorf("zotero api: user ID must be numeric, got %q", creds.UserID)
	}
	switch lib.Scope {
	case zot.LibPersonal:
	case zot.LibShared:
		if !isNumeric(creds.GroupID) {
			return nil, fmt.Errorf("zotero api: shared library needs a numeric group ID, got %q", creds.GroupID)
		}
	default:
		return nil, fmt.Errorf("zotero api: a write targets one library, got scope %q (want %s or %s)",
			lib.Scope, zot.LibPersonal, zot.LibShared)
	}

	c := &Client{
		creds:       creds,
		lib:         lib,
		baseURL:     DefaultBaseURL,
		http:        &http.Client{Timeout: 30 * time.Second},
		maxRetries:  3,
		sleep:       time.Sleep,
		fieldsCache: map[string][]string{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// isNumeric reports whether s is a non-empty string of ASCII digits — the
// shape of every Zotero user and group ID. ParseUint rather than Atoi so a
// sign ("-5", "+5") is rejected too: it would build a path like
// /users/-5 that no library answers to.
func isNumeric(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// Library returns the library this client writes to.
func (c *Client) Library() zot.LibraryRef {
	return c.lib
}

// libraryPrefix is the path segment every library-scoped endpoint starts
// with: /users/{userID} for the personal library, /groups/{groupID} for
// the shared one.
func (c *Client) libraryPrefix() string {
	if c.lib.Scope == zot.LibShared {
		return "/groups/" + c.creds.GroupID
	}
	return "/users/" + c.creds.UserID
}

// Object is one Zotero object as the API returns it: key, version and the
// `data` payload kept generic — sci never models Zotero's per-type schema.
type Object struct {
	Key     string         `json:"key"`
	Version int            `json:"version"`
	Data    map[string]any `json:"data"`
}

// KeyInfo describes the API key the client authenticates with, from
// GET /keys/current: whose key it is and what it may touch.
type KeyInfo struct {
	Key      string
	UserID   int
	Username string
	Access   KeyAccess
}

// KeyAccess is the permission set Zotero attaches to a key. Anything the
// key's `access` object leaves out is false — a key with no `groups` entry
// cannot write to any group, and one with no `access` at all can do
// nothing, which is what setup needs to refuse a read-only key up front.
type KeyAccess struct {
	// UserLibrary is access.user.library: the key can read the personal library.
	UserLibrary bool
	// UserWrite is access.user.write: the key can write the personal library.
	UserWrite bool
	// Groups is access.groups, keyed by numeric group ID as a string — or by
	// the literal "all" when the key was granted every group at once.
	Groups map[string]GroupAccess
}

// GroupAccess is one entry of KeyAccess.Groups.
type GroupAccess struct {
	Library bool `json:"library"`
	Write   bool `json:"write"`
}

// GroupWrite reports whether the key can write to groupID: either through
// the blanket "all" entry or the group's own.
func (a KeyAccess) GroupWrite(groupID string) bool {
	return a.Groups["all"].Write || a.Groups[groupID].Write
}

// keyInfoWire is /keys/current as Zotero sends it. The user block carries
// more flags than sci reads (files, notes); they are dropped on decode.
type keyInfoWire struct {
	Key      string `json:"key"`
	UserID   int    `json:"userID"`
	Username string `json:"username"`
	Access   struct {
		User struct {
			Library bool `json:"library"`
			Write   bool `json:"write"`
		} `json:"user"`
		Groups map[string]GroupAccess `json:"groups"`
	} `json:"access"`
}

// CurrentKey looks up the authenticated key: the quickest way to confirm
// that a key works, which user it belongs to and what it may write. The
// endpoint has no library prefix.
func (c *Client) CurrentKey(ctx context.Context) (*KeyInfo, error) {
	var wire keyInfoWire
	if err := c.getJSON(ctx, "/keys/current", &wire); err != nil {
		return nil, err
	}
	return &KeyInfo{
		Key:      wire.Key,
		UserID:   wire.UserID,
		Username: wire.Username,
		Access: KeyAccess{
			UserLibrary: wire.Access.User.Library,
			UserWrite:   wire.Access.User.Write,
			Groups:      wire.Access.Groups,
		},
	}, nil
}

// response is a fully read HTTP response. Reading the body eagerly keeps
// the retry layer and every caller free of Close bookkeeping.
type response struct {
	Status int
	Header http.Header
	Body   []byte
}

// do sends one request through the retry layer. body, when non-nil, is
// JSON-encoded; extraHeaders are added on top of the two Zotero headers.
func (c *Client) do(ctx context.Context, method, path string, extraHeaders map[string]string, body any) (*response, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%s %s: encode body: %w", method, path, err)
		}
		payload = encoded
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	req.Header.Set("Zotero-API-Key", c.creds.APIKey)
	req.Header.Set("Zotero-API-Version", apiVersion)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.send(req, payload)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return resp, nil
}

// readResponse drains an *http.Response into a response and closes it.
func readResponse(r *http.Response) (*response, error) {
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return &response{Status: r.StatusCode, Header: r.Header, Body: body}, nil
}

// ok reports whether the status is a 2xx.
func (r *response) ok() bool {
	return r.Status >= 200 && r.Status <= 299
}

// httpError turns a non-2xx response into the error callers see. A 413 is
// a [WriteFailedError] because, for a multi-object write, it is the same
// verdict as a failed slot (the payload is unacceptable) delivered at the
// HTTP layer instead; everything else is an [HTTPError].
func (r *response) httpError(method, path string) error {
	body := strings.TrimSpace(string(r.Body))
	if r.Status == http.StatusRequestEntityTooLarge {
		return &WriteFailedError{Code: r.Status, Message: body}
	}
	return &HTTPError{Method: method, Path: path, Status: r.Status, Body: body}
}

// decode unmarshals the response body into out.
func (r *response) decode(method, path string, out any) error {
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// getJSON is GET + decode for endpoints where a 404 is a real error rather
// than a missing object.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	if !resp.ok() {
		return resp.httpError(http.MethodGet, path)
	}
	return resp.decode(http.MethodGet, path, out)
}

// getObject is the single-object GET: a 404 becomes [ErrNotFound].
func (c *Client) getObject(ctx context.Context, path string) (*Object, error) {
	resp, err := c.do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.Status == http.StatusNotFound {
		return nil, fmt.Errorf("GET %s: %w", path, ErrNotFound)
	}
	if !resp.ok() {
		return nil, resp.httpError(http.MethodGet, path)
	}
	var obj Object
	if err := resp.decode(http.MethodGet, path, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// listObjects is a GET that returns an array of objects.
func (c *Client) listObjects(ctx context.Context, path string) ([]Object, error) {
	var objs []Object
	if err := c.getJSON(ctx, path, &objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// writeResponse is the body of a multi-object write. Zotero answers 200
// even when objects were refused: each input index lands in exactly one of
// successful, unchanged or failed.
type writeResponse struct {
	Successful map[string]Object `json:"successful"`
	Failed     map[string]struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"failed"`
}

// createObject POSTs a one-element array to a collection endpoint and
// materialises slot "0" of the response. A failed slot is a
// [WriteFailedError]; the transport-level 413 becomes one in httpError.
//
// Every create carries a fresh Zotero-Write-Token. A POST is not naturally
// idempotent: if the response is lost (timeout, reset) the retry layer
// re-sends it and, without the token, the server would create the object
// twice. Zotero remembers a token for 12 hours and ignores a second
// submission carrying it, so the resend — same request, same token — is
// recognised as the same write.
func (c *Client) createObject(ctx context.Context, path string, data map[string]any) (*Object, error) {
	headers := map[string]string{"Zotero-Write-Token": newWriteToken()}
	resp, err := c.do(ctx, http.MethodPost, path, headers, []map[string]any{data})
	if err != nil {
		return nil, err
	}
	if !resp.ok() {
		return nil, resp.httpError(http.MethodPost, path)
	}
	var result writeResponse
	if err := resp.decode(http.MethodPost, path, &result); err != nil {
		return nil, err
	}
	if failed, ok := result.Failed["0"]; ok {
		return nil, &WriteFailedError{Code: failed.Code, Message: failed.Message}
	}
	created, ok := result.Successful["0"]
	if !ok {
		return nil, fmt.Errorf("POST %s: response carries neither a successful nor a failed slot: %s",
			path, bytes.TrimSpace(resp.Body))
	}
	return &created, nil
}

// newWriteToken returns a Zotero-Write-Token: Zotero wants exactly 32
// characters, which a random UUID rendered as bare hex is.
func newWriteToken() string {
	id := uuid.New()
	return hex.EncodeToString(id[:])
}

// versionHeader is the optimistic-concurrency precondition every PATCH and
// DELETE carries.
func versionHeader(version int) map[string]string {
	return map[string]string{"If-Unmodified-Since-Version": strconv.Itoa(version)}
}

// patchDerived runs a read-modify-write whose payload is a function of the
// object's current server state: GET, derive, PATCH.
//
// The 412 path is why this exists instead of a GET followed by a plain
// PATCH. A conflict means the read the payload was derived from is no
// longer valid — some other writer changed the object — so the recovery is
// to re-read and re-derive, not to stamp a fresh version onto the payload
// already built. Zotero replaces whole arrays on PATCH, so the latter would
// erase whatever the other writer added.
//
// derive returns (patch, needed); needed=false short-circuits with no
// write, which is how "already a member" / "tag not present" become
// genuine no-ops. Only one re-derive is attempted: a second conflict is
// contention worth surfacing as a [VersionConflictError].
func (c *Client) patchDerived(ctx context.Context, path string, derive func(cur *Object) (patch map[string]any, needed bool)) (changed bool, err error) {
	for range 2 {
		cur, err := c.getObject(ctx, path)
		if err != nil {
			return false, err
		}
		patch, needed := derive(cur)
		if !needed {
			return false, nil
		}
		resp, err := c.do(ctx, http.MethodPatch, path, versionHeader(cur.Version), patch)
		if err != nil {
			return false, err
		}
		switch {
		case resp.Status == http.StatusPreconditionFailed:
			continue
		case resp.ok():
			return true, nil
		default:
			return false, resp.httpError(http.MethodPatch, path)
		}
	}
	return false, &VersionConflictError{Path: path}
}

// deleteVersioned reads the object's current version and DELETEs with it,
// re-reading once on a 412 — the same shape as patchDerived with nothing
// to derive.
func (c *Client) deleteVersioned(ctx context.Context, path string) error {
	for range 2 {
		cur, err := c.getObject(ctx, path)
		if err != nil {
			return err
		}
		resp, err := c.do(ctx, http.MethodDelete, path, versionHeader(cur.Version), nil)
		if err != nil {
			return err
		}
		switch {
		case resp.Status == http.StatusPreconditionFailed:
			continue
		case resp.ok():
			return nil
		default:
			return resp.httpError(http.MethodDelete, path)
		}
	}
	return &VersionConflictError{Path: path}
}
