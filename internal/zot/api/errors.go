package api

import (
	"errors"
	"fmt"
)

// ErrNotFound marks a 404 on a single-object GET: the object does not exist
// in the targeted library. Callers use errors.Is to tell a genuine miss
// (data — a batch verb can collect it) from a transport failure (an error,
// never a miss).
var ErrNotFound = errors.New("not found")

// VersionConflictError is a 412 Precondition Failed that survived the one
// re-read-and-retry every versioned write performs: the server copy moved
// twice while we were writing, which signals contention worth surfacing
// rather than spinning on.
type VersionConflictError struct {
	// Path is the request path of the write that conflicted.
	Path string
}

// Error implements the error interface.
func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("version conflict on %s (412 Precondition Failed after one retry)", e.Path)
}

// WriteFailedError is a per-object rejection from a multi-object write
// (POST /items, POST /collections): the request succeeded at the HTTP layer
// but the server refused the object itself, reporting a `failed` slot with
// its own code and message. It also wraps the one whole-request rejection
// that behaves the same way for callers — HTTP 413 (payload too large),
// where no slot is produced at all. Code and Message stay separate fields
// so callers can branch on why the write failed.
type WriteFailedError struct {
	// Code is the server's per-slot error code, or the HTTP status for a
	// whole-request rejection.
	Code int
	// Message is the server's error message, verbatim.
	Message string
}

// Error implements the error interface.
func (e *WriteFailedError) Error() string {
	return fmt.Sprintf("zotero rejected the write (code %d): %s", e.Code, e.Message)
}

// HTTPError is any non-2xx response the client has no more specific name
// for (404 on a single-object GET is [ErrNotFound], 412 after the retry is
// [VersionConflictError], a failed slot or 413 is [WriteFailedError]).
// Body is the response body, trimmed, so the server's own explanation is
// never lost.
type HTTPError struct {
	Method string
	Path   string
	Status int
	Body   string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}
