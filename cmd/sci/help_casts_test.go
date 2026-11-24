package main

import (
	"slices"
	"testing"

	"github.com/sciminds/sci/internal/help"
)

// TestEveryEmbeddedCastIsReachable pins the other half of the cast contract.
//
// casts_test.go asks whether each cast still resolves against the command
// tree — that a recorded demo hasn't gone stale. This asks the inverse: that
// every cast we ship can actually be PLAYED. `sci help` finds a demo by
// deriving a filename from the picker row it is standing on, so a cast whose
// name doesn't match any row is embedded in the binary, rendered to a GIF,
// listed by `just doc-coverage` — and unreachable. Nothing fails; the demo
// simply never appears. A demo for a nested leaf (`sci zot item add`) must
// therefore be recorded under its picker row's name (zot-item.cast).
func TestEveryEmbeddedCastIsReachable(t *testing.T) {
	reachable := map[string]bool{}
	for _, g := range help.BuildGroups(buildRoot()) {
		for _, s := range g.Subs {
			if s.CastFile != "" {
				reachable[s.CastFile] = true
			}
		}
	}

	cmds, err := help.RecordedCommands()
	if err != nil {
		t.Fatalf("RecordedCommands: %v", err)
	}
	if len(cmds) == 0 {
		t.Fatal("no casts found — the parser or the embed is broken")
	}

	var orphans []string
	seen := map[string]bool{}
	for _, rc := range cmds {
		if seen[rc.Cast] {
			continue
		}
		seen[rc.Cast] = true
		if !reachable[rc.Cast] {
			orphans = append(orphans, rc.Cast)
		}
	}

	slices.Sort(orphans)
	for _, name := range orphans {
		t.Errorf("%s is embedded but no `sci help` row resolves to it — it can never be played", name)
	}
}
