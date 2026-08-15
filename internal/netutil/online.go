package netutil

import (
	"net/http"
	"time"
)

// probeURL is a var so tests can redirect to a local httptest server.
var probeURL = "https://captive.apple.com/hotspot-detect.html"

// defaultProbeURL preserves the original for ResetProbeURL.
var defaultProbeURL = probeURL

// SetProbeURL overrides the connectivity probe target. Production never
// calls it: it is the seam that lets tests in other packages (cmd/sci's
// cloud and lab suites, internal/selfupdate) point [Online] at an httptest
// server or a dead port and drive the online-gated code paths without
// touching the network.
func SetProbeURL(url string) { probeURL = url }

// ResetProbeURL restores the default probe target. The test-only
// counterpart to [SetProbeURL]; defer it so package state stays clean.
func ResetProbeURL() { probeURL = defaultProbeURL }

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
