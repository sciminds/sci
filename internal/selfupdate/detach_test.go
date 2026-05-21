//go:build !windows

package selfupdate

import (
	"testing"
	"time"

	"github.com/sciminds/cli/internal/version"
)

// withSpawnFn replaces the package-level spawnFn with a recording stub
// for the duration of the test, then restores it.
func withSpawnFn(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	old := spawnFn
	spawnFn = func(execPath string) error {
		calls = append(calls, execPath)
		return nil
	}
	t.Cleanup(func() { spawnFn = old })
	return &calls
}

func TestSpawnDetachedRefresh_SkipsDevBuild(t *testing.T) {
	withCommit(t, "unknown")
	calls := withSpawnFn(t)
	SpawnDetachedRefresh()
	if len(*calls) != 0 {
		t.Errorf("spawnFn called %d times for dev build; want 0", len(*calls))
	}
}

func TestSpawnDetachedRefresh_SkipsOptOut(t *testing.T) {
	withCommit(t, "abc1234")
	t.Setenv("SCI_NO_UPDATE_CHECK", "1")
	calls := withSpawnFn(t)
	SpawnDetachedRefresh()
	if len(*calls) != 0 {
		t.Errorf("spawnFn called %d times with SCI_NO_UPDATE_CHECK; want 0", len(*calls))
	}
}

// TestSpawnDetachedRefresh_SkipsRecursion is the load-bearing guard against
// the fork-bomb scenario: SpawnDetachedRefresh runs in the parent AND the
// child (the child inherits the parent's setup via main → buildRoot's
// Before hook). Without this check, every detached child would spawn its
// own grandchild ad infinitum.
func TestSpawnDetachedRefresh_SkipsRecursion(t *testing.T) {
	withCommit(t, "abc1234")
	t.Setenv(InternalRefreshEnv, "1")
	calls := withSpawnFn(t)
	SpawnDetachedRefresh()
	if len(*calls) != 0 {
		t.Errorf("spawnFn called %d times in child; want 0 (fork-bomb risk)", len(*calls))
	}
}

func TestSpawnDetachedRefresh_PassesExecutablePath(t *testing.T) {
	withCache(t)
	withCommit(t, "abc1234")
	t.Setenv("SCI_NO_UPDATE_CHECK", "")
	t.Setenv(InternalRefreshEnv, "")
	calls := withSpawnFn(t)
	SpawnDetachedRefresh()
	if len(*calls) != 1 {
		t.Fatalf("spawnFn called %d times; want 1", len(*calls))
	}
	if (*calls)[0] == "" {
		t.Error("spawnFn called with empty execPath; should pass os.Executable() result")
	}
}

// TestSpawnDetachedRefresh_SkipsWhenFresh verifies the rapid-fire dedup:
// three back-to-back `sci foo` invocations should not all spawn refresh
// children. The first stamps LastCheckedAt; subsequent invocations
// within refreshTTL see a fresh cache and skip the spawn.
func TestSpawnDetachedRefresh_SkipsWhenFresh(t *testing.T) {
	withCache(t)
	withCommit(t, "abc1234")
	t.Setenv("SCI_NO_UPDATE_CHECK", "")
	t.Setenv(InternalRefreshEnv, "")

	// Seed the cache as if a refresh just completed.
	writeCache(CheckResult{
		Available:     true,
		LatestSHA:     "bbb",
		LastCheckedAt: time.Now(),
	})

	calls := withSpawnFn(t)
	SpawnDetachedRefresh()
	SpawnDetachedRefresh()
	SpawnDetachedRefresh()
	if len(*calls) != 0 {
		t.Errorf("spawnFn called %d times for fresh cache; want 0", len(*calls))
	}
}

// Compile-time witness that version.Commit assignment is wired correctly.
// (Some test files only touch the env-var paths, which would otherwise
// leave the import unused on a future refactor.)
var _ = version.Commit
