package netutil

import (
	"net/http"
	"time"
)

// probeURL is a var so tests can redirect to a local httptest server.
var probeURL = "https://captive.apple.com/hotspot-detect.html"

// Online performs a fast connectivity check by issuing a HEAD request to a
// well-known, highly available endpoint. Returns true if the host is
// reachable, false on any error (timeout, DNS failure, etc.).
func Online() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Head(probeURL)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
