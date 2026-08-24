package lab

// sshbackend.go — the concrete ssh+rsync backend for lab storage: directory
// listing, size probing, and resumable transfer with --info=progress2 parsing.
// It lives here rather than under internal/tui so the transfer machinery
// carries no dependency on a TUI package; labtui is one caller of it.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// rsyncShutdownGrace is how long to wait between SIGINT and SIGKILL when
// cancelling an in-flight transfer. SIGINT lets rsync flush its --partial
// file cleanly so the next session can resume; SIGKILL is the safety net.
const rsyncShutdownGrace = 2 * time.Second

// SSHBackend shells out to ssh + rsync via the argv builders in this package.
// The user's SSH alias must be configured (sci lab setup) before instantiating
// it.
type SSHBackend struct {
	Cfg *Config
}

// NewSSHBackend returns an SSHBackend that uses the configured SSH alias.
func NewSSHBackend(cfg *Config) *SSHBackend { return &SSHBackend{Cfg: cfg} }

// List runs `ssh alias ls -1FQ --group-directories-first <path>`.
func (s *SSHBackend) List(ctx context.Context, remotePath string) ([]Entry, error) {
	argv := BuildBrowseLsArgs(s.Cfg, remotePath)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ssh ls %s: %w", remotePath, err)
	}
	return ParseLsOutput(string(out)), nil
}

// Size runs `ssh alias du -sbc <paths…>` and parses the total bytes.
func (s *SSHBackend) Size(ctx context.Context, remotePaths []string) (int64, error) {
	argv := BuildSizeArgs(s.Cfg, remotePaths)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return 0, fmt.Errorf("ssh du: %w", err)
	}
	return ParseDuTotal(string(out))
}

// Transfer runs rsync, parses --info=progress2 output line-by-line, and
// pushes each Progress snapshot onto the channel. Returns when rsync exits.
//
// Cancellation: instead of letting exec.CommandContext SIGKILL on ctx.Done
// (the default), we send SIGINT first and only escalate to SIGKILL after a
// grace window. SIGINT is what makes rsync flush its --partial file cleanly,
// which is what the resume-from-manifest flow depends on.
func (s *SSHBackend) Transfer(ctx context.Context, remotePath, localDir string, progress chan<- Progress) error {
	argv := BuildResumableGetArgs(s.Cfg, remotePath, localDir)
	// Plain Command (not CommandContext) so cancellation goes through our
	// graceful-shutdown path below, not the exec package's hard kill.
	cmd := exec.Command(argv[0], argv[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	// Keep stderr separate so rsync's actual error message survives even when
	// stdout is being scanned for --info=progress2 lines.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rsync start: %w", err)
	}

	// Watchdog: when the caller cancels, ask rsync to stop politely; if it
	// still hasn't exited after the grace window, kill it.
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-time.After(rsyncShutdownGrace):
				_ = cmd.Process.Kill()
			case <-watchdogDone:
			}
		case <-watchdogDone:
		}
	}()
	defer close(watchdogDone)

	stdoutTail, scanErr := scanTransferOutput(ctx, stdout, progress)
	waitErr := cmd.Wait()
	return resolveTransferError(remotePath, ctx.Err(), waitErr, scanErr, stderrBuf.String(), stdoutTail)
}

// scanTransferOutput parses rsync's CR- or LF-terminated stdout stream. It
// returns the last eight non-progress lines and any scanner read error. The
// context-aware send prevents a caller that stops draining progress after
// cancellation from pinning the scanner, watchdog, and rsync process.
func scanTransferOutput(ctx context.Context, r io.Reader, progress chan<- Progress) ([]string, error) {
	var stdoutTail []string
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLinesCR)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if p, ok := ParseProgressLine(line); ok {
			select {
			case progress <- p:
			case <-ctx.Done():
				return stdoutTail, ctx.Err()
			}
			continue
		}
		stdoutTail = append(stdoutTail, line)
		if len(stdoutTail) > 8 {
			stdoutTail = stdoutTail[len(stdoutTail)-8:]
		}
	}
	return stdoutTail, scanner.Err()
}

// resolveTransferError deliberately gives caller cancellation precedence over
// process and scanner failures because it states why the transfer stopped. A
// process exit failure is next and remains the first joined cause, with a scan
// failure retained as secondary context. A scan failure by itself is fatal so
// a zero rsync exit cannot report success after an incomplete stdout read.
func resolveTransferError(remotePath string, ctxErr, waitErr, scanErr error, stderr string, stdoutTail []string) error {
	if ctxErr != nil {
		return fmt.Errorf("rsync %s canceled: %w", remotePath, ctxErr)
	}
	if waitErr != nil {
		if scanErr != nil {
			waitErr = errors.Join(waitErr, fmt.Errorf("rsync stdout scan: %w", scanErr))
		}
		detail := strings.TrimSpace(stderr)
		if detail == "" && len(stdoutTail) > 0 {
			detail = strings.Join(stdoutTail, "\n")
		}
		if detail != "" {
			return fmt.Errorf("rsync %s: %w\n%s", remotePath, waitErr, detail)
		}
		return fmt.Errorf("rsync %s: %w", remotePath, waitErr)
	}
	if scanErr != nil {
		if len(stdoutTail) > 0 {
			return fmt.Errorf("rsync %s stdout scan: %w\n%s", remotePath, scanErr, strings.Join(stdoutTail, "\n"))
		}
		return fmt.Errorf("rsync %s stdout scan: %w", remotePath, scanErr)
	}
	return nil
}

// scanLinesCR splits on either \n or \r so rsync's progress2 in-place
// updates produce one token each.
func scanLinesCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
