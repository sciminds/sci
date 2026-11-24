package brew

// outdated_check.go — the bounded "what's out of date?" read. Separate from
// commands.go because it is the one brew call chain a caller must be able to
// give up on: `brew update` is a git fetch against GitHub, and a fetch that
// stalls (captive portal, dead VPN, throttled tap) never returns on its own.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// ErrCheckTimeout reports that a bounded outdated check ran out of time before
// the underlying brew/uv commands answered. Callers should treat it as "the
// upgrade picture is unknown", not "everything is current".
var ErrCheckTimeout = errors.New("outdated check timed out")

// DefaultCheckTimeout bounds [CheckUpdates] when the caller's context carries
// no deadline of its own.
//
// The budget covers a registry refresh (`brew update`, a git fetch of the taps)
// plus two catalogue reads (`brew outdated`, `uv tool list --outdated`). On a
// warm machine that is a few seconds; on a cold one with large taps it can
// reach half a minute. Ninety seconds leaves generous headroom for the slow-but-
// working case while still bounding the stalled one, which is the only case that
// matters here: past this point the fetch is not slow, it is hung, and a
// diagnostic command that never prints is worse than one that reports the
// upgrade section as unknown.
const DefaultCheckTimeout = 90 * time.Second

// OutdatedCheck is the outcome of a registry refresh plus outdated scan. The
// two package sources stay separate because they are upgraded by different
// commands and one failing must not strand the other.
type OutdatedCheck struct {
	Brew []OutdatedPackage // upgradable formulae and casks
	UV   []OutdatedPackage // upgradable uv tools

	// Ignored packages have updates the user asked sci to leave alone.
	Ignored []OutdatedPackage

	// Pinned packages have updates that `brew pin` holds back.
	Pinned []OutdatedPackage
}

// All returns the upgradable brew and uv packages as one slice, brew first.
func (c OutdatedCheck) All() []OutdatedPackage {
	return slices.Concat(c.Brew, c.UV)
}

// CheckUpdates refreshes the Homebrew registry and lists the outdated brew and
// uv packages, giving up when ctx is done. It never installs or upgrades
// anything — pass the result to [UpgradeOnly] or the runner's upgrade methods
// once the user (or an explicit flag) has consented.
//
// When ctx carries no deadline, [DefaultCheckTimeout] is applied, so a caller
// that simply passes its command context still cannot hang.
//
// On timeout the work is *abandoned*, not cancelled: [Runner] has no
// context-aware surface and no handle on the child process, so the in-flight
// `brew update` keeps running until it exits on its own (or the process does).
// That is the deliberate trade — sci's contract is that the command returns and
// prints, and an orphaned read-only subprocess costs nothing next to a CLI that
// produces zero bytes on both streams.
func CheckUpdates(ctx context.Context, r Runner) (OutdatedCheck, error) {
	ignored, err := LoadIgnored()
	if err != nil {
		return OutdatedCheck{}, err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCheckTimeout)
		defer cancel()
	}

	type outcome struct {
		check OutdatedCheck
		err   error
	}
	// Buffered so the abandoned goroutine can finish and exit rather than
	// blocking forever on a send nobody is waiting for.
	done := make(chan outcome, 1)

	go func() {
		if err := r.Update(); err != nil {
			done <- outcome{err: fmt.Errorf("brew update: %w", err)}
			return
		}
		check, err := checkOutdated(r, ignored)
		done <- outcome{check: check, err: err}
	}()

	select {
	case res := <-done:
		return res.check, res.err
	case <-ctx.Done():
		return OutdatedCheck{}, fmt.Errorf("%w: %w", ErrCheckTimeout, ctx.Err())
	}
}
