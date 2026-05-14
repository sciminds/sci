package brew

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// ErrHomebrewInstalled indicates Homebrew is already on PATH; no work to do.
var ErrHomebrewInstalled = errors.New("homebrew is already installed")

// homebrewInstallURL is the official installer location. Kept as a const
// (not a flag/env/config field) so the value cannot be redirected at
// runtime — any change requires a code edit + review.
const homebrewInstallURL = "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"

// maxInstallScriptBytes caps the install script at 1 MiB — Homebrew's
// installer is currently ~30 KB, so anything substantially larger signals
// a wrong URL, an HTML error page, or a misbehaving server.
const maxInstallScriptBytes = 1 << 20

// installScriptClient fetches the installer script. The Timeout bounds the
// download only; the bash run that follows has no timeout because Homebrew
// installation can legitimately take many minutes.
var installScriptClient = &http.Client{Timeout: 30 * time.Second}

// InstallHomebrew downloads the official Homebrew installer and runs it
// non-interactively via `bash -s` (stdin), streaming output to stderr so
// the user sees progress. Returns ErrHomebrewInstalled if brew is already
// on PATH — callers should treat that as a no-op success.
//
// We fetch the script with net/http and pipe it to bash via stdin instead
// of the `bash -c "$(curl ...)"` one-liner Homebrew documents for human
// terminal paste: the one-liner combines `fmt.Sprintf` with `bash -c`,
// which is one refactor away from URL-bytes-as-shell-syntax injection if
// homebrewInstallURL ever becomes runtime-configurable. The Go-side fetch
// removes the shell entirely and lets us shebang-check the body so an
// HTML error page or wrong-URL response isn't executed.
//
// After a successful install, brew lands at /opt/homebrew/bin/brew (Apple
// Silicon) or /usr/local/bin/brew (Intel). It may not yet be on PATH in
// the current process — callers should print the shellenv hint.
func InstallHomebrew() error {
	if _, err := exec.LookPath("brew"); err == nil {
		return ErrHomebrewInstalled
	}
	script, err := fetchInstallScript(homebrewInstallURL, maxInstallScriptBytes)
	if err != nil {
		return fmt.Errorf("download homebrew installer: %w", err)
	}
	cmd := exec.Command("/bin/bash", "-s")
	cmd.Env = append(os.Environ(), "NONINTERACTIVE=1")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// fetchInstallScript GETs url, sanity-checks the response, and returns the
// body bytes. Errors on non-200 status, oversize body, or a body that
// doesn't begin with a bash shebang — guarding against HTML error pages
// or other accidental content being piped to bash.
func fetchInstallScript(url string, maxBytes int64) ([]byte, error) {
	resp, err := installScriptClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("install script larger than %d bytes", maxBytes)
	}
	if !bytes.HasPrefix(body, []byte("#!/bin/bash")) && !bytes.HasPrefix(body, []byte("#!/usr/bin/env bash")) {
		return nil, fmt.Errorf("installer did not return a bash script (first 80 bytes: %q)", firstN(body, 80))
	}
	return body, nil
}

func firstN(b []byte, n int) string {
	if len(b) < n {
		n = len(b)
	}
	return string(b[:n])
}
