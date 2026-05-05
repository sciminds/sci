package lab

import (
	"errors"
	"net"
	"testing"
	"time"
)

// startBannerListener accepts a single connection, writes the given banner,
// then closes. Returns the listener so the test can pass its address to
// PreflightAddr. Closing the listener is the test's responsibility.
func startBannerListener(t *testing.T, banner string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if banner != "" {
			_, _ = conn.Write([]byte(banner))
		}
	}()
	return ln
}

func TestPreflightAddr_Reachable(t *testing.T) {
	ln := startBannerListener(t, "SSH-2.0-fakeserver\r\n")
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

func TestPreflightAddr_NoBanner(t *testing.T) {
	// Listener that accepts and immediately closes — completes the TCP
	// handshake but sends no SSH banner. Mirrors what we see on networks
	// that ACK the SYN but block SSH session traffic.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}()

	err = PreflightAddr(ln.Addr().String(), time.Second)
	if !errors.Is(err, ErrSSHBlocked) {
		t.Errorf("expected ErrSSHBlocked, got %v", err)
	}
}

func TestPreflightAddr_WrongBanner(t *testing.T) {
	// HTTP server stand-in: TCP completes, response arrives, but bytes
	// don't start with "SSH-". This is the captive-portal/transparent-proxy
	// shape we want to reject.
	ln := startBannerListener(t, "HTTP/1.1 200 OK\r\n\r\n")
	defer func() { _ = ln.Close() }()

	err := PreflightAddr(ln.Addr().String(), time.Second)
	if !errors.Is(err, ErrSSHBlocked) {
		t.Errorf("expected ErrSSHBlocked, got %v", err)
	}
}

func TestPreflightAddr_BannerTimeout(t *testing.T) {
	// Accept and hang. Models a network that completes the handshake but
	// then drops/holds SSH traffic until the read deadline expires. The
	// `done` channel keeps the connection open for the lifetime of the
	// test goroutine without needing a fixed sleep.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
	})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		<-done
	}()

	err = PreflightAddr(ln.Addr().String(), 100*time.Millisecond)
	if !errors.Is(err, ErrSSHBlocked) {
		t.Errorf("expected ErrSSHBlocked, got %v", err)
	}
}
