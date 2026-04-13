package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cass/api"
)

const (
	defaultBaseURL       = "https://api.github.com"
	defaultMaxConcurrent = 10
	defaultTimeout       = 30 * time.Second
	apiVersion           = "2022-11-28"
)

// Client is an HTTP client for the GitHub API (including Classroom endpoints).
type Client struct {
	BaseURL       string
	Token         string
	HTTPClient    *http.Client
	MaxConcurrent int

	sem chan struct{} // semaphore for concurrency control
}

// NewClient creates a GitHub API client.
func NewClient(token string) *Client {
	c := &Client{
		BaseURL:       defaultBaseURL,
		Token:         token,
		HTTPClient:    &http.Client{Timeout: defaultTimeout},
		MaxConcurrent: defaultMaxConcurrent,
	}
	c.sem = make(chan struct{}, c.MaxConcurrent)
	return c
}

// Get performs a GET request and decodes the JSON response into dst.
func (c *Client) Get(ctx context.Context, path string, params url.Values, dst any) error {
	rawURL := c.buildURL(path, params)
	return c.getURL(ctx, rawURL, dst)
}

func (c *Client) getURL(ctx context.Context, rawURL string, dst any) error {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GitHub API %s: %d — %s", rawURL, resp.StatusCode, string(body))
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("GitHub API decode: %w", err)
		}
	}
	return nil
}

// GetPaginated performs paginated GET requests, aggregating all pages into dst.
func (c *Client) GetPaginated(ctx context.Context, path string, params url.Values, dst any) error {
	if params == nil {
		params = url.Values{}
	}
	if params.Get("per_page") == "" {
		params.Set("per_page", "100")
	}

	rawURL := c.buildURL(path, params)
	allBytes := make([]json.RawMessage, 0, 100)

	for rawURL != "" {
		pageBytes, nextURL, err := c.fetchPage(ctx, rawURL)
		if err != nil {
			return err
		}
		allBytes = append(allBytes, pageBytes...)
		rawURL = nextURL
	}

	combined, err := json.Marshal(allBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(combined, dst)
}

// GetConcurrent fetches multiple paths concurrently and returns results in order.
func GetConcurrent[T any](ctx context.Context, c *Client, paths []string, params url.Values) ([]T, error) {
	results := make([]T, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)
		go func(idx int, p string) {
			defer wg.Done()
			var result T
			errs[idx] = c.Get(ctx, p, params, &result)
			if errs[idx] == nil {
				results[idx] = result
			}
		}(i, path)
	}
	wg.Wait()

	if err, ok := lo.Find(errs, func(e error) bool { return e != nil }); ok {
		return nil, err
	}
	return results, nil
}

// fetchPage fetches a single page of paginated results, returning raw JSON items
// and the URL for the next page (or "" if none).
func (c *Client) fetchPage(ctx context.Context, rawURL string) ([]json.RawMessage, string, error) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, "", fmt.Errorf("GitHub API %s: %d — %s", rawURL, resp.StatusCode, string(body))
	}

	var page []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("GitHub API decode: %w", err)
	}

	return page, api.ParseNextLink(resp.Header.Get("Link")), nil
}

func (c *Client) buildURL(path string, params url.Values) string {
	u := c.BaseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
}
