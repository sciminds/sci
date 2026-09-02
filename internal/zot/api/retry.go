package api

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

// maxServerWait caps how long the client will pause on a server-supplied
// wait (Retry-After, Backoff). A 429 asking for more than this is handed
// back to the caller untouched — a CLI invocation that would sit silent
// for an hour should fail visibly instead; a Backoff hint above it is
// clamped, since the hint is advisory and the request still succeeded.
const maxServerWait = 60 * time.Second

// send is the retry layer between [Client.do] and the transport. It
// handles the signals Zotero documents for well-behaved clients:
//
//   - 429 Too Many Requests: wait Retry-After seconds (or the backoff
//     schedule when the header is missing), then retry
//   - 5xx: exponential backoff, then retry
//   - network error: exponential backoff, then retry
//   - a Backoff header on ANY response: wait that many seconds before the
//     next request this client sends — a polite hint, honoured once. On a
//     response that is itself retried, the Backoff and the retry delay
//     collapse into one wait, the larger of the two.
//
// Every wait is capped at maxServerWait and interrupted by context
// cancellation. Each retry re-sends body, which is why do buffers the
// payload instead of streaming it. Every other status — 412 included — is
// handed back untouched: recovering from a version conflict needs a re-read
// and a rebuilt payload, which is per-operation knowledge (see
// patchDerived). After maxRetries the last response is returned as-is so
// the caller can turn its status into an error.
func (c *Client) send(req *http.Request, body []byte) (*response, error) {
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := c.wait(ctx, c.takePendingBackoff()); err != nil {
			return nil, err
		}
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
		}

		raw, err := c.http.Do(req)
		if err != nil {
			if attempt >= c.maxRetries || ctx.Err() != nil {
				return nil, err
			}
			if err := c.wait(ctx, backoffDelay(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		resp, err := readResponse(raw)
		if err != nil {
			return nil, err
		}
		hint := min(parseSeconds(resp.Header.Get("Backoff")), maxServerWait)

		var delay time.Duration
		switch {
		case resp.Status == http.StatusTooManyRequests:
			retryAfter := parseSeconds(resp.Header.Get("Retry-After"))
			if retryAfter > maxServerWait {
				return resp, nil
			}
			delay = cmp.Or(retryAfter, backoffDelay(attempt))
		case resp.Status >= 500:
			delay = backoffDelay(attempt)
		default:
			c.setPendingBackoff(hint)
			return resp, nil
		}
		if attempt >= c.maxRetries {
			return resp, nil
		}
		if err := c.wait(ctx, max(delay, hint)); err != nil {
			return nil, err
		}
	}
}

// wait pauses for d through the injected sleep, returning early with
// ctx.Err() if the context is cancelled first. The sleep runs on its own
// goroutine so a cancellation does not have to wait it out; with the real
// time.Sleep that goroutine lingers for at most maxServerWait.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	done := make(chan struct{})
	go func() {
		c.sleep(d)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// takePendingBackoff returns the Backoff hint left by an earlier response
// and clears it, so it is honoured exactly once.
func (c *Client) takePendingBackoff() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := c.pendingWait
	c.pendingWait = 0
	return wait
}

// setPendingBackoff records a Backoff hint for the next request.
func (c *Client) setPendingBackoff(wait time.Duration) {
	if wait <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingWait = wait
}

// backoffDelay is the exponential schedule for retries: 200ms × 2^attempt,
// capped at 10s, no jitter — deterministic so tests can assert on it.
func backoffDelay(attempt int) time.Duration {
	return min(time.Duration(200*(1<<attempt))*time.Millisecond, 10*time.Second)
}

// parseSeconds reads an integer-seconds header value (Retry-After,
// Backoff), returning 0 when it is absent or malformed.
func parseSeconds(v string) time.Duration {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
