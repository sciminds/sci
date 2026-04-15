package lab

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
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

// MasterAlive reports whether a ControlMaster socket for the alias is live.
// `ssh -O check` exits 0 when a master process is reachable, non-zero otherwise.
func MasterAlive(cfg *Config) bool {
	return exec.Command("ssh", "-O", "check", cfg.SSHAlias()).Run() == nil
}

// WarmMaster opens (or reuses) the ControlMaster connection for cfg, prompting
// the user on the real terminal for password / Duo if needed. Once this returns
// nil, subsequent ssh/rsync calls to the same alias tunnel through the master
// for ControlPersist's lifetime — no re-auth, no Duo. Safe to call before
// entering a Bubbletea alt-screen; cheap no-op if a master already exists.
func WarmMaster(cfg *Config) error {
	if MasterAlive(cfg) {
		return nil
	}
	cmd := exec.Command("ssh", cfg.SSHAlias(), "true")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("warm SSH master for %s: %w", cfg.SSHAlias(), err)
	}
	return nil
}
