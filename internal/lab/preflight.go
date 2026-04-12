package lab

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrOffCampus is returned by Preflight when the lab host can't be reached.
// Off-campus, ssrde.ucsd.edu either fails DNS or its SSH port is firewalled,
// so a short TCP dial to :22 is a reliable proxy for "on campus or VPN".
var ErrOffCampus = errors.New("can't reach " + Host + " — looks like you're off campus, connect to the UCSD VPN first")

// preflightTimeout is short enough to feel instant, long enough to tolerate
// a slow campus DNS lookup or first-hop latency.
const preflightTimeout = 3 * time.Second

// preflightAddr is a var so tests can redirect to a local listener.
var preflightAddr = net.JoinHostPort(Host, "22")
var defaultPreflightAddr = preflightAddr

// SetPreflightAddr overrides the preflight target (for tests in other packages).
func SetPreflightAddr(addr string) { preflightAddr = addr }

// ResetPreflightAddr restores the default preflight target.
func ResetPreflightAddr() { preflightAddr = defaultPreflightAddr }

// Preflight verifies the lab SSH host is reachable before invoking ssh/rsync,
// which would otherwise hang for ~75s on the OS connect timeout.
func Preflight() error {
	return PreflightAddr(preflightAddr, preflightTimeout)
}

// PreflightAddr is the testable core of Preflight.
func PreflightAddr(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrOffCampus, err)
	}
	_ = conn.Close()
	return nil
}
