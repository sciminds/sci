package lab

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ErrOffCampus is returned by Preflight when the lab host can't be reached at
// all — DNS failure or TCP timeout/refused. Two scenarios produce this: the
// user is off-campus without VPN, OR the user is on a UCSD-* wifi segment
// (UCSD-PROTECTED, UCSD-GUEST) that firewalls outbound :22 entirely. Both
// fixes are the same — switch to "eduroam" on campus, or connect to VPN.
var ErrOffCampus = errors.New("can't reach " + Host + " — make sure you're on \"eduroam\" or connected to the UCSD VPN")

// ErrSSHBlocked is returned when TCP to :22 succeeds but no SSH identification
// banner arrives. Some campus networks (notably UCSD-PROTECTED / UCSD-GUEST)
// complete the TCP handshake but block or RST SSH session traffic, so a TCP-
// only preflight passes while real ssh/rsync fails. Banner-grab catches that
// state up-front instead of letting users land in a setup loop where ssh-copy-id
// succeeds but the verification connection is rejected.
var ErrSSHBlocked = errors.New("reached " + Host + ":22 but no SSH banner came back — your network is blocking SSH. Make sure you're on \"eduroam\" or connected to the UCSD VPN")

// preflightTimeout bounds both the dial and the banner read. Short enough to
// feel instant, long enough to tolerate slow campus DNS or first-hop latency.
const preflightTimeout = 3 * time.Second

// preflightAddr is a var so tests can redirect to a local listener.
var preflightAddr = net.JoinHostPort(Host, "22")
var defaultPreflightAddr = preflightAddr

// SetPreflightAddr overrides the preflight target (for tests in other packages).
func SetPreflightAddr(addr string) { preflightAddr = addr }

// ResetPreflightAddr restores the default preflight target.
func ResetPreflightAddr() { preflightAddr = defaultPreflightAddr }

// Preflight verifies the lab SSH host is reachable AND that the network passes
// SSH end-to-end before invoking ssh/rsync.
func Preflight() error {
	return PreflightAddr(preflightAddr, preflightTimeout)
}

// PreflightAddr is the testable core of Preflight. Two-stage check:
//  1. TCP dial — fails fast off-campus (ErrOffCampus).
//  2. Read the SSH identification banner. RFC 4253 §4.2 has the server send
//     "SSH-<protoversion>-<softwareversion>\r\n" before the client speaks,
//     capped at 255 bytes. If we don't see "SSH-" in the first read, the
//     network is dropping SSH session traffic (ErrSSHBlocked) — the
//     symptom that lured users into the setup loop.
func PreflightAddr(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrOffCampus, err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(deadline)

	buf := make([]byte, 255)
	n, readErr := conn.Read(buf)
	if n > 0 && strings.HasPrefix(string(buf[:n]), "SSH-") {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("%w (%v)", ErrSSHBlocked, readErr)
	}
	return ErrSSHBlocked
}
