package app

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sciminds/cli/internal/lab"
)

// SSHBackend implements Backend by shelling out to ssh + rsync via the
// argv builders in the lab package. The user's SSH alias must be configured
// (sci lab setup) before instantiating this backend.
type SSHBackend struct {
	Cfg *lab.Config
}

// NewSSHBackend returns a Backend that uses the configured SSH alias.
func NewSSHBackend(cfg *lab.Config) *SSHBackend { return &SSHBackend{Cfg: cfg} }

// List runs `ssh alias ls -1FA --group-directories-first <path>`.
func (s *SSHBackend) List(ctx context.Context, remotePath string) ([]lab.Entry, error) {
	argv := lab.BuildBrowseLsArgs(s.Cfg, remotePath)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return nil, fmt.Errorf("ssh ls %s: %w", remotePath, err)
	}
	return lab.ParseLsOutput(string(out)), nil
}

// Size runs `ssh alias du -sbc <paths…>` and parses the total bytes.
func (s *SSHBackend) Size(ctx context.Context, remotePaths []string) (int64, error) {
	argv := lab.BuildSizeArgs(s.Cfg, remotePaths)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return 0, fmt.Errorf("ssh du: %w", err)
	}
	return lab.ParseDuTotal(string(out))
}

// Transfer runs rsync, parses --info=progress2 output line-by-line, and
// pushes each Progress snapshot onto the channel. Returns when rsync exits.
func (s *SSHBackend) Transfer(ctx context.Context, remotePath, localDir string, progress chan<- lab.Progress) error {
	argv := lab.BuildResumableGetArgs(s.Cfg, remotePath, localDir)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rsync start: %w", err)
	}

	// rsync's --info=progress2 emits CR-terminated updates on the same line.
	// Use a custom scanner split that breaks on either '\n' or '\r' so we
	// see every progress tick.
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanLinesCR)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if p, ok := lab.ParseProgressLine(line); ok {
			select {
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				return ctx.Err()
			case progress <- p:
			}
		}
	}
	return cmd.Wait()
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
