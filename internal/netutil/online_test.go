package netutil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnline_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 OK, minimal response.
	}))
	defer srv.Close()

	old := probeURL
	probeURL = srv.URL
	defer func() { probeURL = old }()

	if !Online() {
		t.Fatal("Online() = false, want true when probe succeeds")
	}
}

func TestOnline_ServerDown(t *testing.T) {
	// Start and immediately close so the port is unreachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	old := probeURL
	probeURL = url
	defer func() { probeURL = old }()

	if Online() {
		t.Fatal("Online() = true, want false when server is down")
	}
}

func TestOnline_DNSFailure(t *testing.T) {
	old := probeURL
	probeURL = "http://this-host-does-not-exist.invalid"
	defer func() { probeURL = old }()

	if Online() {
		t.Fatal("Online() = true, want false on DNS failure")
	}
}

func TestOnline_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := probeURL
	probeURL = srv.URL
	defer func() { probeURL = old }()

	// Online() only checks for an error from client.Head(); a 500 response
	// is still a successful HTTP round-trip, so Online() returns true.
	if !Online() {
		t.Fatal("Online() = false, want true even on 500 (connection succeeded)")
	}
}

func TestOnline_InvalidURL(t *testing.T) {
	old := probeURL
	probeURL = "://bad-url"
	defer func() { probeURL = old }()

	if Online() {
		t.Fatal("Online() = true, want false for malformed URL")
	}
}
