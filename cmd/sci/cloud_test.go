package main

import (
	"testing"
)

// TestCloud_SubcommandShape locks in the post-alignment surface: ls/get/put
// /browse/remove (mirrors `sci lab`). The actions themselves are exercised
// by integration tests; here we just guard the command shape so a typo or
// rename never silently regresses the contract with `sci lab`.
func TestCloud_SubcommandShape(t *testing.T) {
	root := buildRoot()
	cloud := findCmd(root.Commands, "cloud")
	if cloud == nil {
		t.Fatal("cloud command not found")
	}

	want := []struct {
		name    string
		aliases []string
	}{
		{"setup", nil},
		{"ls", []string{"list"}},
		{"get", nil},
		{"put", nil},
		{"browse", nil},
		{"remove", []string{"rm"}},
	}
	for _, w := range want {
		sub := findCmd(cloud.Commands, w.name)
		if sub == nil {
			t.Errorf("cloud %s subcommand missing", w.name)
			continue
		}
		for _, a := range w.aliases {
			found := false
			for _, got := range sub.Aliases {
				if got == a {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("cloud %s missing alias %q (have %v)", w.name, a, sub.Aliases)
			}
		}
	}

	// ls/browse must both accept --public; lab's TUI/CLI split has the
	// same flag on both halves, so cloud should too.
	for _, name := range []string{"ls", "browse"} {
		sub := findCmd(cloud.Commands, name)
		if sub == nil {
			continue
		}
		if !hasFlag(sub, "public") {
			t.Errorf("cloud %s should have a --public flag", name)
		}
	}
}
