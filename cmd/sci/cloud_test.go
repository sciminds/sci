package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/netutil"
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
			found := slices.Contains(sub.Aliases, a)
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

// TestCloud_BrowseRedirectsToGet locks in the user-facing redirect for the
// removed `sci cloud browse` command. Muscle-memory typing should land on a
// tailored "use sci cloud get instead" message, not the generic Levenshtein
// suggestion (which used to surface "remove" — actively misleading).
func TestCloud_BrowseRedirectsToGet(t *testing.T) {
	// netutil.Online runs in cloudCommand's Before, but RejectUnknownSubcommand
	// is chained ahead of it via WireNamespaceDefaults, so the redirect short-
	// circuits before any network probe. Stubbing online anyway keeps this
	// test honest if that wiring order ever flips.
	netutil.SetProbeURL("http://127.0.0.1:1")
	t.Cleanup(netutil.ResetProbeURL)

	err := buildRoot().Run(context.Background(), []string{"sci", "cloud", "browse"})
	if err == nil {
		t.Fatal("expected deprecation error")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Errorf("error should say the command was removed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "sci cloud get") {
		t.Errorf("error should point to `sci cloud get`, got: %v", err)
	}
}
