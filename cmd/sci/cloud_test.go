package main

import (
	"testing"
)

// TestCloud_SubcommandShape locks in the surface: setup/ls/get/put/remove.
// `get` with no arg opens the interactive browser (folded in from the old
// `browse` subcommand). The actions themselves are exercised by integration
// tests; here we just guard the command shape so a typo or rename never
// silently regresses it.
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

	// `browse` is gone; its --public flag now lives on `get`, which serves
	// both the CLI download and the interactive-browser entry point.
	for _, name := range []string{"ls", "get"} {
		sub := findCmd(cloud.Commands, name)
		if sub == nil {
			continue
		}
		if !hasFlag(sub, "public") {
			t.Errorf("cloud %s should have a --public flag", name)
		}
	}

	if findCmd(cloud.Commands, "browse") != nil {
		t.Errorf("cloud browse should be removed; its functionality moved to `sci cloud get`")
	}
}
