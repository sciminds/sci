//go:build !windows

package selfupdate

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/sciminds/cli/internal/version"
)

// spawnFn is the indirection used by [SpawnDetachedRefresh] so tests can
// observe the spawn arguments without actually forking a process.
var spawnFn = startDetachedChild

// SpawnDetachedRefresh launches a child `sci` process that performs the
// update check, then returns immediately. Unlike `go RefreshCache()`,
// the child survives the parent's exit, so short commands (`sci proj`,
// `sci db list`) actually complete the network call and the cache stays
// fresh.
//
// No-ops in four cases:
//   - dev build (version.Commit == "unknown") — no release to compare against.
//   - SCI_NO_UPDATE_CHECK set — explicit user opt-out.
//   - SCI_INTERNAL_REFRESH_UPDATE_CACHE=1 — we ARE the child; recursing
//     would fork-bomb the cache refresh.
//   - cache was refreshed within [refreshTTL] — preserves GitHub API
//     quota and dedupes rapid-fire invocations (`sci a && sci b && sci c`).
//
// All failures are swallowed: this is best-effort background maintenance,
// never something that should impede the user's actual command.
func SpawnDetachedRefresh() {
	if version.Commit == "unknown" {
		return
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return
	}
	if os.Getenv(InternalRefreshEnv) == "1" {
		return
	}
	if cacheIsFresh() {
		return
	}
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	_ = spawnFn(execPath)
}

// startDetachedChild forks an os-level detached child running execPath
// with [InternalRefreshEnv] set. Setsid makes the child a new session
// leader so it survives the parent's exit; stdio is nil'd so the child
// cannot write to the parent's terminal (it would corrupt user-visible
// output otherwise).
func startDetachedChild(execPath string) error {
	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(), InternalRefreshEnv+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release the child immediately. cmd.Wait would block until the child
	// exits, which defeats the purpose. The Go runtime won't reap zombies
	// for us, but Setsid + nil stdio means the kernel reparents to init
	// (PID 1) once the parent exits, and init reaps.
	_ = cmd.Process.Release()
	return nil
}
