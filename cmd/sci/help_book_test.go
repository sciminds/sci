package main

import (
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/help"
)

// TestEveryCommandBookHasAnOverview pins the one thing `sci help` cannot
// derive from the command tree.
//
// A book's name, its one-line usage and its subcommand list all come from
// the live tree, so they follow a rename for free. The wiki-style
// overview above them does not — it is hand-written in
// internal/help/descs.go and keyed by command name, and a missing key is
// a plain map miss that renders as an empty block. That is how `sci
// setup` shipped as the only Getting Started command with no description
// at all: nothing failed, the panel was simply blank.
func TestEveryCommandBookHasAnOverview(t *testing.T) {
	for _, g := range help.BuildGroups(buildRoot()) {
		if strings.TrimSpace(g.LongDesc) == "" {
			t.Errorf("`sci %s` has no overview — add a longDescs entry in internal/help/descs.go", g.Name)
		}
	}
}
