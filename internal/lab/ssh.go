package lab

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

// SafeReadPath resolves a relative path under ReadRoot.
// Rejects absolute paths and directory traversal.
func SafeReadPath(rel string) (string, error) {
	if rel == "" || rel == "." {
		return ReadRoot, nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("invalid path: %q (must be relative)", rel)
	}
	cleaned := path.Clean(rel)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid path: %q (cannot escape root)", rel)
	}
	return path.Join(ReadRoot, cleaned), nil
}

// SafeWritePath resolves a relative path under the user's write directory.
// Rejects absolute paths and directory traversal.
func SafeWritePath(cfg *Config, rel string) (string, error) {
	writeDir := cfg.WriteDir()
	if rel == "" || rel == "." {
		return writeDir, nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("invalid path: %q (must be relative)", rel)
	}
	cleaned := path.Clean(rel)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid path: %q (cannot escape write directory)", rel)
	}
	return path.Join(writeDir, cleaned), nil
}

// BuildLsArgs constructs the argv for listing a remote directory.
func BuildLsArgs(cfg *Config, remotePath string) []string {
	return []string{"ssh", cfg.SSHAlias(), "ls", "-1lh", ShellQuote(remotePath)}
}

// BuildGetArgs constructs the argv for downloading via rsync.
// `-s` (secluded-args) sends paths through the rsync protocol instead of the
// remote shell, so spaces or shell metacharacters in remotePath are safe.
func BuildGetArgs(cfg *Config, remotePath, localPath string) []string {
	return []string{"rsync", "-avz", "-s", "--progress", cfg.SSHAlias() + ":" + remotePath, localPath}
}

// BuildPutArgs constructs the argv for uploading via rsync.
// If dryRun is true, --dry-run is appended so rsync only shows what would transfer.
// `-s` protects remotePath from remote-shell reinterpretation.
func BuildPutArgs(cfg *Config, localPath, remotePath string, dryRun bool) []string {
	args := []string{"rsync", "-avz", "-s", "--progress"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, localPath, cfg.SSHAlias()+":"+remotePath)
	return args
}

// ShellQuote wraps s in single quotes, escaping any embedded single quotes,
// so it survives reinterpretation by the remote login shell when passed as a
// trailing argument to ssh. Use whenever a path or value derived from
// directory listings (or any other untrusted source) flows into an ssh argv.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildOpenArgs constructs the argv for an interactive SSH shell in the user's write directory.
func BuildOpenArgs(cfg *Config) []string {
	return []string{"ssh", "-t", cfg.SSHAlias(), "cd " + ReadRoot + " && exec $SHELL -l"}
}

// masterCheckTimeout bounds how long we wait for a quick "true" through the
// ControlMaster before declaring the connection stale.
const masterCheckTimeout = 10 * time.Second

// MasterAlive reports whether a ControlMaster socket for the alias is live.
// `ssh -O check` exits 0 when a master process is reachable, non-zero otherwise.
func MasterAlive(cfg *Config) bool {
	return exec.Command("ssh", "-O", "check", cfg.SSHAlias()).Run() == nil
}

// MasterHealthy returns true when a live ControlMaster can actually reach the
// remote host. A master process can survive a network drop — the socket stays
// but the TCP connection is dead. Running a trivial command with BatchMode+timeout
// detects this without prompting for auth.
func MasterHealthy(cfg *Config) bool {
	if !MasterAlive(cfg) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), masterCheckTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", cfg.SSHAlias(), "true").Run() == nil
}

// KillMaster tears down the ControlMaster for cfg. Errors are ignored because
// the socket may already be gone.
func KillMaster(cfg *Config) {
	_ = exec.Command("ssh", "-O", "exit", cfg.SSHAlias()).Run()
}

// WarmMaster opens (or reuses) the ControlMaster connection for cfg, prompting
// the user on the real terminal for password / Duo if needed. Once this returns
// nil, subsequent ssh/rsync calls to the same alias tunnel through the master
// for ControlPersist's lifetime — no re-auth, no Duo. Safe to call before
// entering a Bubbletea alt-screen; cheap no-op if a healthy master exists.
//
// If the master process is alive but the underlying TCP connection is dead
// (e.g. after a network drop), it kills the stale master and re-authenticates.
func WarmMaster(cfg *Config) error {
	if MasterHealthy(cfg) {
		return nil
	}
	// Kill any stale master so a fresh connection can take over.
	KillMaster(cfg)

	fmt.Fprintf(os.Stderr, "Connecting to %s (you may be prompted for your password and Duo 2FA)…\n", Host)
	cmd := exec.Command("ssh", cfg.SSHAlias(), "true")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SSH authentication failed for %s: %w", cfg.SSHAlias(), err)
	}
	return nil
}
