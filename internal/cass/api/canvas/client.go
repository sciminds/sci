package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sciminds/cli/internal/cass/api"
)

const (
	defaultThrottleThreshold = 50.0
	defaultMaxRetries        = 3
	defaultRetryBaseDelay    = time.Second
	defaultThrottleDelay     = time.Second
	defaultTimeout           = 30 * time.Second
)

// FormData is a map of form field names to values for POST/PUT requests.
// Keys use Canvas bracket notation (e.g. "module[name]").
type FormData map[string]string

// Client is an HTTP client for the Canvas LMS REST API.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client

	// Tuning knobs — exported for testing. Defaults are set by NewClient.
	ThrottleThreshold float64       // X-Rate-Limit-Remaining below this triggers a delay (default 50)
	ThrottleDelay     time.Duration // How long to pause when throttled (default 1s)
	MaxRetries        int           // Max attempts on 429/network error (default 3)
	RetryBaseDelay    time.Duration // Base delay for exponential backoff (default 1s)

	// Mutable state — protected by mu.
	mu            sync.Mutex
	throttleUntil time.Time
}

// NewClient creates a canvas API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:           strings.TrimRight(baseURL, "/"),
		Token:             token,
		HTTPClient:        &http.Client{Timeout: defaultTimeout},
		ThrottleThreshold: defaultThrottleThreshold,
		ThrottleDelay:     defaultThrottleDelay,
		MaxRetries:        defaultMaxRetries,
		RetryBaseDelay:    defaultRetryBaseDelay,
	}
}

// Get performs a GET request and decodes the JSON response into dst.
func (c *Client) Get(ctx context.Context, path string, params url.Values, dst any) error {
	return c.do(ctx, "GET", path, params, nil, dst)
}

// GetPaginated performs paginated GET requests, collecting all pages.
// It returns the raw JSON pages; use GetAllPaginated for typed results.
func (c *Client) GetPaginated(ctx context.Context, path string, params url.Values, dst any) error {
	if params == nil {
		params = url.Values{}
	}
	if params.Get("per_page") == "" {
		params.Set("per_page", "100")
	}

	fullURL := c.buildURL(path, params)
	return c.getPaginatedURL(ctx, fullURL, dst)
}

func (c *Client) getPaginatedURL(ctx context.Context, rawURL string, dst any) error {
	// Collect all raw JSON arrays, then decode into dst.
	allBytes := make([]json.RawMessage, 0, 100)

	for rawURL != "" {
		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return err
		}
		c.setHeaders(req)
		c.waitThrottle()

		resp, err := c.doWithRetry(req)
		if err != nil {
			return fmt.Errorf("canvas API: %w", err)
		}

		c.checkRateLimit(resp)

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return fmt.Errorf("canvas API %s: %d — %s", rawURL, resp.StatusCode, string(body))
		}

		var page []json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			_ = resp.Body.Close()
			return fmt.Errorf("canvas API decode: %w", err)
		}
		_ = resp.Body.Close()

		allBytes = append(allBytes, page...)
		rawURL = api.ParseNextLink(resp.Header.Get("Link"))
	}

	// Marshal collected items back and decode into the typed dst slice.
	combined, err := json.Marshal(allBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(combined, dst)
}

// PostForm sends a POST with form-encoded body.
func (c *Client) PostForm(ctx context.Context, path string, form FormData, dst any) error {
	return c.doForm(ctx, "POST", path, form, dst)
}

// PutForm sends a PUT with form-encoded body.
func (c *Client) PutForm(ctx context.Context, path string, form FormData, dst any) error {
	return c.doForm(ctx, "PUT", path, form, dst)
}

// Delete sends a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, "DELETE", path, nil, nil, nil)
}

// BulkGradeForm builds a FormData for the update_grades endpoint.
// grades maps Canvas user IDs to grade strings.
func BulkGradeForm(grades map[int]string) FormData {
	form := make(FormData, len(grades))
	for uid, grade := range grades {
		key := fmt.Sprintf("grade_data[%d][posted_grade]", uid)
		form[key] = grade
	}
	return form
}

// --- internals ---

func (c *Client) do(ctx context.Context, method, path string, params url.Values, body io.Reader, dst any) error {
	rawURL := c.buildURL(path, params)
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	c.waitThrottle()

	resp, err := c.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("canvas API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.checkRateLimit(resp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("canvas API %s %s: %d — %s", method, path, resp.StatusCode, string(respBody))
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("canvas API decode: %w", err)
		}
	}
	return nil
}

func (c *Client) doForm(ctx context.Context, method, path string, form FormData, dst any) error {
	vals := url.Values{}
	for k, v := range form {
		vals.Set(k, v)
	}
	encoded := vals.Encode()

	rawURL := c.buildURL(path, nil)
	req, err := http.NewRequestWithContext(ctx, method, rawURL, strings.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req)
	c.waitThrottle()

	resp, err := c.doWithRetry(req)
	if err != nil {
		return fmt.Errorf("canvas API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.checkRateLimit(resp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("canvas API %s %s: %d — %s", method, path, resp.StatusCode, string(respBody))
	}

	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return fmt.Errorf("canvas API decode: %w", err)
		}
	}
	return nil
}

func (c *Client) buildURL(path string, params url.Values) string {
	u := c.BaseURL + "/api/v1" + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Accept", "application/json")
	}
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := range c.MaxRetries {
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			delay := c.retryDelay(resp, attempt)
			time.Sleep(delay)
			lastErr = fmt.Errorf("rate limited (429)")
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

func (c *Client) retryDelay(resp *http.Response, attempt int) time.Duration {
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return c.RetryBaseDelay * time.Duration(1<<attempt)
}

func (c *Client) checkRateLimit(resp *http.Response) {
	remaining := resp.Header.Get("X-Rate-Limit-Remaining")
	if remaining == "" {
		return
	}
	val, err := strconv.ParseFloat(remaining, 64)
	if err != nil {
		return
	}
	if val < c.ThrottleThreshold {
		c.mu.Lock()
		c.throttleUntil = time.Now().Add(c.ThrottleDelay)
		c.mu.Unlock()
	}
}

func (c *Client) waitThrottle() {
	c.mu.Lock()
	until := c.throttleUntil
	c.mu.Unlock()
	if !until.IsZero() && time.Now().Before(until) {
		time.Sleep(time.Until(until))
	}
}
