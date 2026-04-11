package netutil

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestWrap_nil(t *testing.T) {
	t.Parallel()
	if got := Wrap("upload", nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestWrap_nonNetwork(t *testing.T) {
	t.Parallel()
	err := errors.New("file not found")
	got := Wrap("upload", err)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	// Should preserve the original error via %w.
	if !strings.Contains(got.Error(), "file not found") {
		t.Fatalf("expected original message preserved, got %q", got)
	}
	// Should NOT contain the network hint.
	if strings.Contains(got.Error(), hint) {
		t.Fatalf("non-network error should not get hint, got %q", got)
	}
}

func TestWrap_dnsError(t *testing.T) {
	t.Parallel()
	err := &net.DNSError{Err: "no such host", Name: "example.com"}
	got := Wrap("checking for updates", err)
	if !strings.Contains(got.Error(), "DNS lookup failed") {
		t.Fatalf("expected DNS message, got %q", got)
	}
	if !strings.Contains(got.Error(), hint) {
		t.Fatalf("expected hint, got %q", got)
	}
}

func TestWrap_connectionRefused(t *testing.T) {
	t.Parallel()
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	got := Wrap("upload", err)
	if !strings.Contains(got.Error(), "could not connect") {
		t.Fatalf("expected connection message, got %q", got)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestWrap_timeout(t *testing.T) {
	t.Parallel()
	got := Wrap("download", timeoutErr{})
	if !strings.Contains(got.Error(), "timed out") {
		t.Fatalf("expected timeout message, got %q", got)
	}
}

func TestWrap_stringHeuristics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errMsg   string
		wantSnip string
	}{
		{"dial tcp: lookup foo.com: no such host", "DNS lookup failed"},
		{"read tcp: connection reset by peer", "connection reset"},
		{"dial tcp 1.2.3.4:443: network is unreachable", "network is unreachable"},
		{"net/http: TLS handshake timeout", "TLS handshake timed out"},
		{"x509: certificate signed by unknown authority", "TLS certificate error"},
	}
	for _, tt := range tests {
		got := Wrap("op", errors.New(tt.errMsg))
		if !strings.Contains(got.Error(), tt.wantSnip) {
			t.Errorf("for %q: expected %q in %q", tt.errMsg, tt.wantSnip, got)
		}
	}
}
