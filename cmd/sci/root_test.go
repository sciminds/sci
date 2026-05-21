package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/selfupdate"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/version"
	"github.com/urfave/cli/v3"
)

// setupNoticeEnv redirects the selfupdate cache to a tempdir, pins
// version.Commit, and disables the detached refresh spawn so the test does
// not fork its own test binary as a refresh worker. Returns the canonical
// cache file path inside the tempdir (which may or may not exist yet).
func setupNoticeEnv(t *testing.T) string {
	t.Helper()

	cacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	origCommit := version.Commit
	version.Commit = "aaaaaaa1111111"
	t.Cleanup(func() { version.Commit = origCommit })

	// SpawnDetachedRefresh checks this env var first and bails — exactly
	// what we want in tests, where exec'ing the test binary as a refresh
	// child would re-run the suite recursively.
	t.Setenv(selfupdate.InternalRefreshEnv, "1")

	// uikit's quiet flag is package-level state; Before resets it but
	// belt-and-suspenders for early-failure paths.
	t.Cleanup(func() { uikit.SetQuiet(false) })

	return filepath.Join(cacheDir, "sci", "update-check.json")
}

func writeNoticeCache(t *testing.T, path string, result selfupdate.CheckResult) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	os.Stderr = origStderr
	_ = w.Close()
	<-done
	return buf.String()
}

// TestRootBefore_RendersNoticeAndMarks verifies the load-bearing path: a
// fresh cache with an available update produces a stderr notice on the next
// `sci ...` invocation, and MarkNoticeShown stamps LastShownAt so the
// next read suppresses.
func TestRootBefore_RendersNoticeAndMarks(t *testing.T) {
	path := setupNoticeEnv(t)
	checkedAt := time.Now().UTC().Truncate(time.Second)
	writeNoticeCache(t, path, selfupdate.CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: checkedAt,
	})

	stderr := captureStderr(t, func() {
		if err := buildRoot().Run(context.Background(), []string{"sci"}); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	if !strings.Contains(stderr, "Update available") {
		t.Errorf("stderr missing notice; got %q", stderr)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache file missing after run: %v", err)
	}
	var got selfupdate.CheckResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("cache corrupt: %v", err)
	}
	if !got.LastShownAt.After(got.LastCheckedAt) {
		t.Errorf("LastShownAt (%v) not after LastCheckedAt (%v); MarkNoticeShown did not run",
			got.LastShownAt, got.LastCheckedAt)
	}
}

// TestRootBefore_JSONSuppressesNotice asserts that --json keeps stderr
// clean. Scripts and LLMs piping --json output must not see a stray
// "Update available" line on stderr breaking diagnostic capture.
func TestRootBefore_JSONSuppressesNotice(t *testing.T) {
	path := setupNoticeEnv(t)
	writeNoticeCache(t, path, selfupdate.CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: time.Now(),
	})

	stderr := captureStderr(t, func() {
		_ = buildRoot().Run(context.Background(), []string{"sci", "--json"})
	})

	if strings.Contains(stderr, "Update available") {
		t.Errorf("stderr contains notice under --json: %q", stderr)
	}
}

// TestRootBefore_UpdateSubcommandSuppressesNotice asserts that `sci update`
// does not double-announce — the user is already running the updater.
// Detection uses cmd.Args().First() because root's Before receives the
// root command, not the resolved subcommand.
//
// The real update.Action calls uikit.RunWithSpinner which spawns a
// goroutine to run its callback; tea.Program's internal sync edge isn't
// visible to the race detector, so mocking selfupdateCheck/Update/Exec
// and running the real Action via buildRoot triggers a flaky race in CI
// (caught on Go 1.26 darwin/arm64). We don't need to exercise runUpdate
// at all here — swap update.Action to a no-op so Before still fires with
// the same Args().First() but no spinner goroutine spawns.
func TestRootBefore_UpdateSubcommandSuppressesNotice(t *testing.T) {
	path := setupNoticeEnv(t)
	writeNoticeCache(t, path, selfupdate.CheckResult{
		Available:     true,
		LatestSHA:     "bbbbbbb2222222",
		LastCheckedAt: time.Now(),
	})

	root := buildRoot()
	for _, c := range root.Commands {
		if c.Name == "update" {
			c.Action = func(context.Context, *cli.Command) error { return nil }
			c.Before = nil
		}
	}

	stderr := captureStderr(t, func() {
		_ = root.Run(context.Background(), []string{"sci", "update"})
	})

	if strings.Contains(stderr, "Update available") {
		t.Errorf("stderr contains notice under `sci update`: %q", stderr)
	}
}

// TestRootBefore_EmptyCacheIsNoop asserts the no-cache path is silent and
// does not panic — covers fresh installs that have never refreshed.
func TestRootBefore_EmptyCacheIsNoop(t *testing.T) {
	setupNoticeEnv(t)
	// No writeNoticeCache — cache file does not exist.

	stderr := captureStderr(t, func() {
		if err := buildRoot().Run(context.Background(), []string{"sci"}); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	if strings.Contains(stderr, "Update available") {
		t.Errorf("stderr contains notice with no cache: %q", stderr)
	}
}
