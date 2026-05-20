package brew

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// InstallHomebrew downloads the official Homebrew installer and runs it,
// streaming output to stderr so the user sees progress. Returns
// ErrHomebrewInstalled if brew is already on PATH — callers should treat
// that as a no-op success.
//
// We fetch the script with net/http and execute it from a temp file instead
// of the `bash -c "$(curl ...)"` one-liner Homebrew documents for human
// terminal paste: the one-liner combines `fmt.Sprintf` with `bash -c`,
// which is one refactor away from URL-bytes-as-shell-syntax injection if
// homebrewInstallURL ever becomes runtime-configurable. The Go-side fetch
// removes the shell entirely and lets us shebang-check the body so an
// HTML error page or wrong-URL response isn't executed.
//
// The script is run from a temp file (not piped through stdin) so the
// installer's own stdin is the user's terminal. Homebrew's installer
// shells out to `sudo` for the initial chown of /opt/homebrew (or
// /usr/local on Intel); sudo reads the password from /dev/tty, which
// requires the process to be attached to a terminal. NONINTERACTIVE=1
// is intentionally NOT set — that flag makes Homebrew invoke `sudo -n`
// (non-interactive), which fails immediately when a password is needed.
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
	scriptPath, cleanup, err := writeTempScript(script)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeTempScript persists script bytes to a temp file marked executable
// and returns the path plus a cleanup func that removes it. Used so the
// Homebrew installer can be invoked with stdin attached to the user's
// terminal — sudo's password prompt needs /dev/tty, which only works when
// the process inherits the terminal as stdin.
func writeTempScript(script []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "homebrew-install-*.sh")
	if err != nil {
		return "", nil, fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := io.Copy(f, bytes.NewReader(script)); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write installer to %s: %w", filepath.Base(path), err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close installer file: %w", err)
	}
	return path, cleanup, nil
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
