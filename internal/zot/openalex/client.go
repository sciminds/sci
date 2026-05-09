package openalex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL        = "https://api.openalex.org"
	defaultTimeout        = 30 * time.Second
	defaultMaxRetries     = 3
	defaultRetryBaseDelay = time.Second
	// defaultMaxRetryDelay caps any single retry sleep, including
	// server-supplied Retry-After. OpenAlex very occasionally returns
	// multi-thousand-second values which would otherwise wedge a long batch
	// scan with no progress. 60s is generous enough for a real backoff
	// without making the failure mode look like a hang.
	defaultMaxRetryDelay = 60 * time.Second
	userAgent            = "sci-zot (+https://github.com/sciminds/cli)"
)

// Client is a thin HTTP client for the OpenAlex API.
type Client struct {
	BaseURL    string
	Email      string // mailto (polite pool); optional
	APIKey     string // premium key; optional
	HTTPClient *http.Client

	MaxRetries     int
	RetryBaseDelay time.Duration
	// MaxRetryDelay caps the per-attempt sleep on retry, including any
	// Retry-After value sent by the server. Zero falls back to
	// defaultMaxRetryDelay; set explicitly in tests for fast paths.
	MaxRetryDelay time.Duration
}

// NewClient creates an OpenAlex client. Both email and apiKey are optional —
// pass empty strings to use the anonymous tier.
func NewClient(email, apiKey string) *Client {
	return &Client{
		BaseURL:        defaultBaseURL,
		Email:          email,
		APIKey:         apiKey,
		HTTPClient:     &http.Client{Timeout: defaultTimeout},
		MaxRetries:     defaultMaxRetries,
		RetryBaseDelay: defaultRetryBaseDelay,
		MaxRetryDelay:  defaultMaxRetryDelay,
	}
}

// Get performs a GET request and decodes the JSON response into dst.
func (c *Client) Get(ctx context.Context, path string, params url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.buildURL(path, params), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("openalex: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("openalex %s: %d — %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("openalex decode: %w", err)
		}
	}
	return nil
}

func (c *Client) buildURL(path string, params url.Values) string {
	if params == nil {
		params = url.Values{}
	}
	if c.Email != "" && params.Get("mailto") == "" {
		params.Set("mailto", c.Email)
	}
	if c.APIKey != "" && params.Get("api_key") == "" {
		params.Set("api_key", c.APIKey)
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := range c.MaxRetries {
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			delay := c.retryDelay(resp, attempt)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(delay):
			}
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("max retries exceeded")
	}
	return nil, lastErr
}

func (c *Client) retryDelay(resp *http.Response, attempt int) time.Duration {
	maxDelay := c.MaxRetryDelay
	if maxDelay <= 0 {
		maxDelay = defaultMaxRetryDelay
	}
	var d time.Duration
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			d = time.Duration(secs) * time.Second
		}
	}
	if d == 0 {
		d = c.RetryBaseDelay * time.Duration(1<<attempt)
	}
	return min(d, maxDelay)
}
