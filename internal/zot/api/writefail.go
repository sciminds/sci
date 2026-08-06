package api

import (
	"fmt"
	"net/http"
	"strings"
)

// WriteFailedError is a per-object rejection from a multi-object write
// (`POST /items` / `POST /collections` / `POST /searches`): the request
// succeeded at the HTTP layer but the server refused the object itself,
// reporting a failed slot with its own code and message. It also wraps
// the one whole-request rejection that behaves the same way for callers
// — HTTP 413 (payload too large), where no slot is produced at all.
//
// Carrying the server's code and message as fields (rather than
// flattening them into an opaque string) lets callers branch on *why*
// the write failed — see [WriteFailedError.NoteTooLong].
type WriteFailedError struct {
	// Index is the failed slot's key ("0" for single-object writes), or
	// "" when the rejection was HTTP-level rather than per-slot.
	Index string
	// Code is the server's per-slot error code (or the HTTP status for
	// a whole-request rejection).
	Code int
	// Message is the server's error message, verbatim.
	Message string
}

func (e *WriteFailedError) Error() string {
	if e.Index == "" {
		return fmt.Sprintf("write rejected (code %d): %s", e.Code, e.Message)
	}
	return fmt.Sprintf("batch item %s failed: %s", e.Index, e.Message)
}

// NoteTooLong reports whether this rejection is Zotero's note-length
// limit — a permanent verdict on this body, not a transient failure.
// Retrying the identical payload can never succeed; callers should
// record the rejection and stop resubmitting (extract-lib does exactly
// that). Zotero signals it as slot code 413 ("Note '…' too long"), and
// oversize payloads can also die at the HTTP layer as a whole-request
// 413.
func (e *WriteFailedError) NoteTooLong() bool {
	return e.Code == http.StatusRequestEntityTooLarge ||
		strings.Contains(strings.ToLower(e.Message), "too long")
}
