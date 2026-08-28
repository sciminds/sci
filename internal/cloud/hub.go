package cloud

// hub.go — sci's direct line to the Hugging Face Hub HTTP API.
//
// The `hf` CLI keeps the jobs only it can do: holding the credential
// (`hf auth login`) and moving bytes, where its xet chunking and dedup are the
// whole point. Read-only metadata goes straight to the Hub instead, because the
// CLI's own overhead dwarfs the requests it wraps — `hf auth whoami` measured at
// 70-125s and a recursive `hf buckets ls` at 53-121s, against endpoints that
// answer the same questions in ~0.3s and ~0.2s on the same connection.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// hubMaxPages caps Link-header following. The Hub ends its own pagination; this
// only stops a malformed or cyclic `next` link from looping forever.
const hubMaxPages = 100

// hubGet issues an authenticated GET against an absolute Hub URL.
func hubGet(ctx context.Context, rawURL string) (*http.Response, error) {
	tok, err := hfToken()
	if err != nil || tok == "" {
		return nil, errNoToken
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build hugging face request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hugging face request: %w", err)
	}
	return resp, nil
}

// hubStatusError maps a non-OK Hub response to an error, or nil when the status
// is fine. Auth failures are worded so [isNotLoggedIn] recognizes them and
// [Verify] can answer [ErrNotAuthenticated].
func hubStatusError(resp *http.Response, what string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		// A stored-but-rejected token leaves the user at the same next step as
		// no token at all: run `hf auth login`.
		return fmt.Errorf("not logged in to Hugging Face: the stored token was rejected (%s)", resp.Status)
	default:
		return fmt.Errorf("%s: %s", what, resp.Status)
	}
}

// hubNextPage returns the next page's URL from a Link header, or "" when this
// was the last page. The Hub paginates with RFC 8288 `<url>; rel="next"`.
func hubNextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		segments := strings.Split(part, ";")
		if len(segments) < 2 || !strings.Contains(segments[1], `rel="next"`) {
			continue
		}
		return strings.Trim(strings.TrimSpace(segments[0]), "<>")
	}
	return ""
}

// hfEndpoint returns the Hub base URL, honoring HF_ENDPOINT — the same override
// huggingface_hub uses to target a private or enterprise Hub.
func hfEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("HF_ENDPOINT")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://huggingface.co"
}

// bucketTreeURL builds the listing URL for a bucket prefix. The prefix is a
// single path segment as far as the API is concerned, so it is escaped whole —
// a space or a `#` in a folder name must not split the URL.
func bucketTreeURL(org, bucket, prefix string) string {
	u := hfEndpoint() + "/api/buckets/" + org + "/" + bucket + "/tree"
	if p := strings.Trim(prefix, "/"); p != "" {
		u += "/" + url.PathEscape(p)
	}
	return u + "?recursive=true"
}

// listBucketTree returns every file and folder under prefix (empty = the whole
// bucket), following the Hub's pagination. An absent prefix reads as empty
// rather than as a failure: callers use this to ask "is anything here?".
func listBucketTree(ctx context.Context, org, bucket, prefix string) ([]hfFile, error) {
	var all []hfFile
	next := bucketTreeURL(org, bucket, prefix)
	for page := 0; next != "" && page < hubMaxPages; page++ {
		files, link, err := fetchBucketPage(ctx, next)
		if err != nil {
			return nil, err
		}
		all = append(all, files...)
		next = link
	}
	return all, nil
}

// fetchBucketPage reads one page of a bucket listing, returning its entries and
// the URL of the page after it (empty when there is none).
func fetchBucketPage(ctx context.Context, rawURL string) (files []hfFile, next string, err error) {
	resp, err := hubGet(ctx, rawURL)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil // prefix doesn't exist — an empty listing, not an error
	}
	if err := hubStatusError(resp, "listing bucket"); err != nil {
		return nil, "", err
	}
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, "", fmt.Errorf("parse bucket listing: %w", err)
	}
	return files, hubNextPage(resp.Header.Get("Link")), nil
}
