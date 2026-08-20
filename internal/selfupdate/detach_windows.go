//go:build windows

package selfupdate

import (
	"os"
)

// SpawnDetachedRefresh on Windows falls back to the previous goroutine
// pattern: `syscall.SysProcAttr.Setsid` is Unix-only and the project
// does not currently ship Windows binaries. If you ever change that,
// implement true detachment here via DETACHED_PROCESS / CREATE_NEW_PROCESS_GROUP.
func SpawnDetachedRefresh() {
	if updateChecksDisabled() {
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
