package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// hubRetryMaxAttempts is four total tries: the initial request and three
	// retries. All Hub requests here are GETs, so replay is idempotent. Four
	// attempts absorb short outages without turning a failing command into a
	// long retry loop.
	hubRetryMaxAttempts = 4

	// hubRetryBaseDelay produces deterministic 250ms, 500ms, and 1s waits.
	// Deterministic backoff keeps tests exact; the small attempt count avoids
	// synchronized clients retrying for long enough to warrant jitter.
	hubRetryBaseDelay = 250 * time.Millisecond

	// hubRetryAfterCap limits server-directed waits to five seconds. This is
	// long enough to honor ordinary rate-limit guidance while leaving useful
	// room in whoami's ten-second deadline and rejecting hostile day-long waits.
	hubRetryAfterCap = 5 * time.Second
)

// hubRetryNow and hubRetrySleep are package seams for deterministic tests.
// Production sleeps remain context-aware so cancellation interrupts backoff.
var (
	hubRetryNow   = time.Now
	hubRetrySleep = sleepHubRetry
)

// hubDo sends an idempotent Hub GET with at most [hubRetryMaxAttempts] total
// attempts. It retries transport failures and the transient HTTP statuses
// recognized by [hubRetryableStatus], draining every discarded response body
// before reusing the connection. A wait interrupted by the context returns a
// wrapped context error and no response; a wait the deadline cannot afford
// returns the last real failure without closing its response body.
func hubDo(ctx context.Context, req *http.Request) (*http.Response, error) {
	for attempt := range hubRetryMaxAttempts {
		response, err := hubHTTPClient.Do(req)
		if !hubShouldRetry(ctx, response, err) || attempt+1 == hubRetryMaxAttempts {
			return response, err
		}

		delay := hubRetryDelay(response, attempt)
		if !hubCanWait(ctx, delay) {
			return response, err
		}
		if response != nil {
			drainAndCloseHubResponse(response)
		}
		if waitErr := hubRetrySleep(ctx, delay); waitErr != nil {
			cause := ctx.Err()
			if cause == nil {
				cause = waitErr
			}
			return nil, fmt.Errorf("wait to retry hugging face request: %w", cause)
		}
	}
	return nil, errors.New("hugging face retry loop ended unexpectedly")
}

// hubShouldRetry allows transport errors unless the caller's context has
// already ended, and otherwise delegates the response status decision.
func hubShouldRetry(ctx context.Context, response *http.Response, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		return true
	}
	return response != nil && hubRetryableStatus(response.StatusCode)
}

// hubRetryableStatus is deliberately narrow: authentication, absence, and
// every other client rejection must surface without delay.
func hubRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// hubRetryDelay returns exponential backoff unless a 429 or 503 supplies a
// valid Retry-After delta or HTTP-date, in which case the capped server delay
// wins. Invalid values fall back to the computed delay.
func hubRetryDelay(response *http.Response, attempt int) time.Duration {
	backoff := hubRetryBaseDelay << attempt
	if response == nil || (response.StatusCode != http.StatusTooManyRequests && response.StatusCode != http.StatusServiceUnavailable) {
		return backoff
	}

	raw := strings.TrimSpace(response.Header.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
		if seconds >= int64(hubRetryAfterCap/time.Second) {
			return hubRetryAfterCap
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return backoff
	}
	return min(max(when.Sub(hubRetryNow()), 0), hubRetryAfterCap)
}

// hubCanWait rejects a backoff that cannot finish within the caller's current
// deadline. The last response or transport error is more useful than sleeping
// into a timeout whose outcome is already known.
func hubCanWait(ctx context.Context, delay time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || !hubRetryNow().Add(delay).After(deadline)
}

// drainAndCloseHubResponse releases a response that will not be returned to
// the caller. Draining permits net/http to reuse the keep-alive connection.
func drainAndCloseHubResponse(response *http.Response) {
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

// sleepHubRetry waits for delay or returns as soon as the context ends.
func sleepHubRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
