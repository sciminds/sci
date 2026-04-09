// Package netutil provides user-friendly network error wrapping.
package netutil

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// hint is the standard suffix appended to wrapped network errors.
const hint = "check your internet connection and try again"

// Wrap inspects err for common network failure patterns and returns a
// user-friendly message. Non-network errors are returned unchanged.
// The action string (e.g. "upload", "checking for updates") is used to
// prefix the message.
func Wrap(action string, err error) error {
	if err == nil {
		return nil
	}
	if msg := friendly(err); msg != "" {
		return fmt.Errorf("%s: %s — %s", action, msg, hint)
	}
	return fmt.Errorf("%s: %w", action, err)
}

// friendly returns a short human-readable diagnosis, or "" if the error
// is not a recognisable network problem.
func friendly(err error) string {
	// Context deadline / timeout.
	if isTimeout(err) {
		return "request timed out"
	}

	// DNS resolution failure.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS lookup failed"
	}

	// Connection refused / reset / unreachable.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "could not connect"
	}

	// Catch-all heuristics on the error string for wrapped errors that
	// lost their type (e.g. from AWS SDK or nested fmt.Errorf chains).
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "DNS lookup failed"
	case strings.Contains(s, "connection refused"):
		return "connection refused"
	case strings.Contains(s, "connection reset"):
		return "connection reset"
	case strings.Contains(s, "network is unreachable"):
		return "network is unreachable"
	case strings.Contains(s, "i/o timeout"):
		return "request timed out"
	case strings.Contains(s, "TLS handshake timeout"):
		return "TLS handshake timed out"
	case strings.Contains(s, "certificate"),
		strings.Contains(s, "x509"):
		return "TLS certificate error"
	}

	return ""
}

// isTimeout returns true if any error in the chain implements Timeout().
func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
