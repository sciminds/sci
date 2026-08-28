package lab

// sshbackend.go — the concrete ssh+rsync backend for lab storage: directory
// listing, size probing, and resumable transfer with --info=progress2 parsing.
// It lives here rather than under internal/tui so the transfer machinery
// carries no dependency on a TUI package; labtui is one caller of it.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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

	// rsync's --info=progress2 emits CR-terminated updates on the same line.
	// Use a custom scanner split that breaks on either '\n' or '\r' so we
	// see every progress tick. Non-progress lines (filenames, warnings) are
	// kept in stdoutTail to surface alongside stderr if rsync exits non-zero.
	var stdoutTail []string
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanLinesCR)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if p, ok := ParseProgressLine(line); ok {
			progress <- p
			continue
		}
		stdoutTail = append(stdoutTail, line)
		if len(stdoutTail) > 8 {
			stdoutTail = stdoutTail[len(stdoutTail)-8:]
		}
	}
	waitErr := cmd.Wait()
	// Surface the cancellation reason rather than rsync's exit-by-signal
	// status, so callers see context.Canceled / DeadlineExceeded.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := waitErr; err != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" && len(stdoutTail) > 0 {
			detail = strings.Join(stdoutTail, "\n")
		}
		if detail != "" {
			return fmt.Errorf("rsync %s: %w\n%s", remotePath, err, detail)
		}
		return fmt.Errorf("rsync %s: %w", remotePath, err)
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
