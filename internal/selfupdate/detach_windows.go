//go:build windows

package selfupdate

import (
	"os"

	"github.com/sciminds/cli/internal/version"
)

// SpawnDetachedRefresh on Windows falls back to the previous goroutine
// pattern: `syscall.SysProcAttr.Setsid` is Unix-only and the project
// does not currently ship Windows binaries. If you ever change that,
// implement true detachment here via DETACHED_PROCESS / CREATE_NEW_PROCESS_GROUP.
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
	go RefreshCache()
}
