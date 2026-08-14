package cli

import (
	"context"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// treeRunMu serializes runs of the production command tree.
//
// urfave/cli writes a flag's parsed value through its Destination
// pointer, and FlagBase.PreParse does so for every declared flag's
// default on every run — not just the flags that appeared on the command
// line. A Destination aimed at a package-level variable therefore makes
// two concurrently-parsing trees write the same memory, which `go test
// -race` (CI's gate, not the local `just ok` one) reports as a data race
// even though the assertions all pass.
//
// This package no longer aims a Destination anywhere: each Action reads
// its flags off the parsed *cli.Command. The lock stays while the rest of
// the repo is swept, so the tables that exercise the tree can keep
// t.Parallel. Sequential tests need not take it: Go holds every parallel
// test until the package's sequential ones have finished, so the two
// groups never overlap.
var treeRunMu sync.Mutex

// lockProductionFlags takes treeRunMu for the rest of t, for a parallel
// test that has to build its own tree out of Commands() instead of using
// runZotTree.
func lockProductionFlags(t *testing.T) {
	t.Helper()
	treeRunMu.Lock()
	t.Cleanup(treeRunMu.Unlock)
}

// runZotTree builds the production tree under a bare `zot` root, the way
// cmd/sci/zot.go mounts it, and runs argv through it.
//
// The library Before hook is deliberately not installed: every assertion
// in these tests lands during flag parsing or inside a stub Action, so
// no config or database is needed.
//
// Run the tree through this helper and nothing else — a bare
// `root.Run(...)` inside a parallel test reintroduces the race described
// on treeRunMu.
func runZotTree(t *testing.T, argv ...string) error {
	t.Helper()
	treeRunMu.Lock()
	defer treeRunMu.Unlock()
	root := &cli.Command{Name: "zot", Flags: PersistentFlags(), Commands: Commands()}
	return root.Run(context.Background(), argv)
}
