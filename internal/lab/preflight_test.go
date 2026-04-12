package lab

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestPreflightAddr_Reachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := PreflightAddr(ln.Addr().String(), time.Second); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestPreflightAddr_Unreachable(t *testing.T) {
	// 192.0.2.0/24 is TEST-NET-1 (RFC 5737) — guaranteed non-routable.
	err := PreflightAddr("192.0.2.1:22", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrOffCampus) {
		t.Errorf("expected ErrOffCampus, got %v", err)
	}
}
