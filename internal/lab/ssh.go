package lab

import (
	"fmt"
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
	return []string{"ssh", cfg.SSHAlias(), "ls", "-1lh", remotePath}
}

// BuildGetArgs constructs the argv for downloading via rsync.
func BuildGetArgs(cfg *Config, remotePath, localPath string) []string {
	return []string{"rsync", "-avz", "--progress", cfg.SSHAlias() + ":" + remotePath, localPath}
}

// BuildPutArgs constructs the argv for uploading via rsync.
// If dryRun is true, --dry-run is appended so rsync only shows what would transfer.
func BuildPutArgs(cfg *Config, localPath, remotePath string, dryRun bool) []string {
	args := []string{"rsync", "-avz", "--progress"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, localPath, cfg.SSHAlias()+":"+remotePath)
	return args
}

// BuildOpenArgs constructs the argv for an interactive SSH shell in the user's write directory.
func BuildOpenArgs(cfg *Config) []string {
	return []string{"ssh", "-t", cfg.SSHAlias(), "cd " + ReadRoot + " && exec $SHELL -l"}
}
