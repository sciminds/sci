// Package connector speaks to Zotero desktop's local HTTP server on
// 127.0.0.1:23119. It is the in-process drag-drop equivalent: callers push a
// PDF through /connector/saveStandaloneAttachment, desktop imports it into
// whichever library is currently selected in the UI, and — when
// Zotero.Prefs.get('autoRecognizeFiles') is on — fires the same
// recognize-document pipeline a browser connector save would trigger.
//
// We deliberately do not speak the connector protocol in full (no
// getTranslators, saveItems, document-integration surface). This package
// covers only what `zot import` needs: ping, upload, poll for the recognized
// parent.
//
// The endpoint is undocumented for third-party use. Zotero maintainers have
// called it "not really intended for external consumption," so it can change
// without API-version bumps. We keep the surface minimal and include the
// X-Zotero-Connector-API-Version header to stay aligned with real connector
// traffic.
package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

// DefaultBaseURL is Zotero desktop's local HTTP server. The server binds to
// 127.0.0.1 only (server.js: `serv.init(23119, true, -1)` with the
// "loopback-only" flag), so there is no hostname variant — always localhost.
const DefaultBaseURL = "http://127.0.0.1:23119"

// defaultUserAgent intentionally does NOT start with "Mozilla/" — desktop's
// server.js treats any Mozilla-prefixed UA as a browser and then demands a
// Zotero-Allowed-Request or X-Zotero-Connector-API-Version header to pass the
// CSRF guard (server.js:407–424). We send the API version header anyway for
// belt-and-suspenders, but keeping the UA non-Mozilla sidesteps the guard
// even if the header check ever tightens.
const defaultUserAgent = "sci-zot-connector"

// connectorAPIVersion is the value the real browser connector sends. Bumps
// would be coordinated with desktop's server_connector.js; until we see one
// in the wild, 3 matches current desktop.
const connectorAPIVersion = "3"

// ErrDesktopUnreachable wraps network-layer failures so callers can branch on
// "desktop is not running" without string-matching. Used by Ping and by the
// other methods when the connection itself fails (pre-TLS, pre-first-byte).
var ErrDesktopUnreachable = errors.New("zotero desktop connector is unreachable")

// HTTPDoer matches the subset of *http.Client we need. Exposed so tests can
// substitute, though the httptest.Server form in client_test.go runs the real
// client against a real server and exercises the wire format end-to-end.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is the connector HTTP client. Zero value is not valid — use NewClient.
type Client struct {
	baseURL string
	ua      string
	doer    HTTPDoer
}

// Option configures Client. Follows the same pattern as the api package.
type Option func(*Client)

// WithBaseURL overrides the default http://127.0.0.1:23119. Primarily for
// tests that wire an httptest.Server; production callers use the default.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient substitutes the HTTP doer. Useful for tests that want to
// intercept at the round-tripper level; production uses http.DefaultClient.
func WithHTTPClient(d HTTPDoer) Option { return func(c *Client) { c.doer = d } }

// NewClient builds a Client targeting Zotero desktop's local server.
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		ua:      defaultUserAgent,
		doer:    http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Ping issues a GET /connector/ping. A 2xx means desktop is running and
// willing to talk to us. Network errors are wrapped with ErrDesktopUnreachable
// so callers can distinguish "not running" from "running but returned 4xx".
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/connector/ping", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.doer.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDesktopUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ping: status %d", resp.StatusCode)
	}
	return nil
}

// SaveMeta is the metadata the browser connector would ordinarily derive from
// the page it's saving. For a local-file import we synthesize plausible
// values: URL as file:// for traceability, Title as the basename. SessionID
// must be unique per upload — the caller is expected to generate a UUID.
type SaveMeta struct {
	SessionID string
	URL       string
	Title     string
}

// SaveResp is the decoded response from /connector/saveStandaloneAttachment.
// Desktop returns exactly one field in the JSON body (see
// server_connector.js: saveStandaloneAttachment handler): whether the
// uploaded attachment is a candidate for auto-recognition. The attachment's
// Zotero itemKey is NOT returned here — callers who need it must either poll
// getRecognizedItem (happy path, gets the parent) or reconcile via the local
// /api/ surface.
type SaveResp struct {
	CanRecognize bool `json:"canRecognize"`
}

// SaveStandaloneAttachment POSTs the raw PDF bytes to desktop. The body is
// plain application/pdf (not multipart, not form-encoded) — desktop reads it
// as a stream via importFromNetworkStream. The metadata ride-along is a JSON
// blob in the X-Metadata header.
//
// The body is fully buffered into memory before the POST so we can set an
// explicit Content-Length. Desktop's saveStandaloneAttachment handler rejects
// chunked transfer with 400 "Content-length not provided" — net/http falls
// back to chunked whenever the body reader isn't a *bytes.Reader / *bytes.Buffer
// / *strings.Reader, so a *os.File body from Import would fail without this
// buffering. PDFs in practice are a few MB; streaming multi-GB uploads would
// need a different approach (we'd have to Stat the file and wire the size
// through without reading), but that isn't a real workload here.
//
// When CanRecognize is true, desktop has kicked off the auto-recognize
// pipeline internally (session.autoRecognizePromise is set); call
// GetRecognizedItem with the same SessionID to await the result. When false,
// the PDF is still imported but no recognition ran.
func (c *Client) SaveStandaloneAttachment(ctx context.Context, body io.Reader, meta SaveMeta) (*SaveResp, error) {
	metaJSON, err := json.Marshal(map[string]string{
		"sessionID": meta.SessionID,
		"url":       meta.URL,
		"title":     meta.Title,
	})
	if err != nil {
		return nil, fmt.Errorf("encode X-Metadata: %w", err)
	}

	buf, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read attachment body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/connector/saveStandaloneAttachment", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	// bytes.NewReader sets Content-Length via NewRequest's type assertion,
	// but set it explicitly for belt-and-suspenders in case anyone swaps the
	// reader type later.
	req.ContentLength = int64(len(buf))
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("X-Metadata", string(metaJSON))
	req.Header.Set("X-Zotero-Connector-API-Version", connectorAPIVersion)

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDesktopUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("saveStandaloneAttachment: status %d: %s", resp.StatusCode, string(b))
	}

	var out SaveResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode saveStandaloneAttachment response: %w", err)
	}
	return &out, nil
}

// RecognizedResp is the decoded response from /connector/getRecognizedItem.
// Wire format verified against desktop main (server_connector.js:GetRecognizedItem):
// the handler awaits session.autoRecognizePromise then returns either
// 200 + {"title": "...", "itemType": "..."} (recognition produced a parent)
// or 204 No Content (recognition finished but produced no item).
//
// The handler intentionally does NOT include the Zotero item key in the
// response body. Callers who want the key must reconcile via a separate
// library lookup (by title or by querying items added since a pre-upload
// version snapshot). See the package doc comment for why v1 skips that.
type RecognizedResp struct {
	Recognized bool   `json:"-"`
	Title      string `json:"title"`
	ItemType   string `json:"itemType"`
}

// GetRecognizedItem asks desktop for the result of the recognize session.
// The call blocks on the server side until recognition completes — do NOT
// treat this as a polling endpoint. Supply a ctx with a timeout for the
// whole operation instead.
//
// A 204 return indicates recognition completed but no parent item was
// produced (the PDF didn't match CrossRef/arXiv identifiers). This is
// distinct from a transport error and should be surfaced to the user as
// "recognition ran but couldn't identify the document".
func (c *Client) GetRecognizedItem(ctx context.Context, sessionID string) (*RecognizedResp, error) {
	reqBody, err := json.Marshal(map[string]string{"sessionID": sessionID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/connector/getRecognizedItem", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Zotero-Connector-API-Version", connectorAPIVersion)

	resp, err := c.doer.Do(req)
	if err != nil {
		var nerr net.Error
		if errors.As(err, &nerr) {
			return nil, fmt.Errorf("%w: %v", ErrDesktopUnreachable, err)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		// Recognition finished but produced no parent item — treat as
		// "recognized=false" at the API layer and let the caller decide
		// how to present it.
		return &RecognizedResp{Recognized: false}, nil
	case http.StatusOK:
		var out RecognizedResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("decode getRecognizedItem response: %w", err)
		}
		out.Recognized = out.Title != "" || out.ItemType != ""
		return &out, nil
	default:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("getRecognizedItem: status %d: %s", resp.StatusCode, string(b))
	}
}

// BuildFileURL returns a file:// URL for the given absolute filesystem path.
// Used to populate SaveMeta.URL without the caller having to hand-roll URL
// encoding. Kept exported for the Import orchestrator and any future caller.
func BuildFileURL(absPath string) string {
	u := &url.URL{Scheme: "file", Path: absPath}
	return u.String()
}
