package cli

import (
	"context"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// treeRunMu serializes runs of the production command tree.
//
// urfave/cli parks a flag's parsed value in the package-level variable
// named by its Destination (doctorDeep, missingLimit, searchLimit, …),
// and FlagBase.PreParse writes every declared flag's default on every
// run — not just the flags that appeared on the command line. Two trees
// parsing concurrently therefore write the same memory, which `go test
// -race` (CI's gate, not the local `just ok` one) reports as a data
// race even though the assertions all pass.
//
// The tables that exercise the tree are worth keeping t.Parallel, so the
// runs take this lock instead. Sequential tests need not: Go holds every
// parallel test until the package's sequential ones have finished, so
// the two groups never overlap.
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
