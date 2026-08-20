package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/sciminds/sci/internal/cass/api"
)

const (
	defaultBaseURL = "https://api.github.com"
	defaultTimeout = 30 * time.Second
	apiVersion     = "2022-11-28"
)

// Client is an HTTP client for the GitHub API (including Classroom endpoints).
// Requests run sequentially — pagination is a chain where each page names the
// next — so the client carries no concurrency control.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient creates a GitHub API client.
func NewClient(token string) *Client {
	return &Client{
		BaseURL:    defaultBaseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
	}
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

// fetchPage fetches a single page of paginated results, returning raw JSON items
// and the URL for the next page (or "" if none).
func (c *Client) fetchPage(ctx context.Context, rawURL string) ([]json.RawMessage, string, error) {
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
