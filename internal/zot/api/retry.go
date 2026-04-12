package api

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"
)

// retryDoer is the HTTP middleware wrapping the raw transport. It handles:
//
//   - 429 Too Many Requests: honor Retry-After, then retry up to maxRetry
//   - 5xx Server Errors: exponential backoff, retry up to maxRetry
//   - Backoff response header: sleep that many seconds BEFORE the next request
//     this doer sees (polite hint)
//
// 412 Precondition Failed is NOT retried here — that requires fetching the
// latest version and re-sending the payload, which is per-operation logic
// (see withVersionRetry in items.go).
type retryDoer struct {
	inner     HTTPDoer
	client    *Client
	honorWait time.Duration // carried forward from a previous Backoff header
}

// Do implements HTTPDoer with retry and backoff handling.
func (r *retryDoer) Do(req *http.Request) (*http.Response, error) {
	// If the server previously asked us to back off, honor it BEFORE sending.
	if r.honorWait > 0 {
		r.client.sleep(r.honorWait)
		r.honorWait = 0
	}

	// Buffer body for retries (Zotero POSTs are small — at most 50 items).
	var bodyBytes []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		bodyBytes = b
	}

	var resp *http.Response
	var err error
	for attempt := 0; attempt <= r.client.maxRetry; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		resp, err = r.inner.Do(req)
		if err != nil {
			// Network-level error: retry with exponential backoff.
			if attempt == r.client.maxRetry {
				return nil, err
			}
			r.client.sleep(backoffDelay(attempt))
			continue
		}

		// Record Backoff header for the NEXT request regardless of status.
		if bo := parseSeconds(resp.Header.Get("Backoff")); bo > 0 {
			r.honorWait = bo
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter := parseSeconds(resp.Header.Get("Retry-After"))
			if retryAfter == 0 {
				retryAfter = backoffDelay(attempt)
			}
			_ = drainAndClose(resp)
			if attempt == r.client.maxRetry {
				// Synthesize a 429 response for the caller to see.
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     resp.Header,
					Body:       io.NopCloser(bytes.NewReader(nil)),
					Request:    req,
				}, nil
			}
			r.client.sleep(retryAfter)
			continue
		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			_ = drainAndClose(resp)
			if attempt == r.client.maxRetry {
				return resp, nil
			}
			r.client.sleep(backoffDelay(attempt))
			continue
		default:
			return resp, nil
		}
	}
	return resp, err
}

// backoffDelay returns an exponential backoff (no jitter) of 200ms * 2^attempt,
// capped at 10s. Tests override time.Sleep so zero-duration is effectively free.
func backoffDelay(attempt int) time.Duration {
	d := time.Duration(200*(1<<attempt)) * time.Millisecond
	if d > 10*time.Second {
		d = 10 * time.Second
	}
	return d
}

// parseSeconds parses an integer seconds header value, returning 0 on empty
// or malformed input.
func parseSeconds(v string) time.Duration {
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// drainAndClose ensures the response body is fully drained before closing so
// keep-alive connections are reusable.
func drainAndClose(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}
