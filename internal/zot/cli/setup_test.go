package cli

// `sci zot setup` records one thing — where zotero.sqlite lives — and holds
// no credential of any kind. The flag that forgets it was called --logout,
// four lines above help text explaining that there is no account to log
// into. It is --clear, and the rename is deliberate: no alias, no shim, so
// a script passing the old name fails loudly instead of half-working.

import (
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// flagNames collects every name and alias a command declares.
func flagNames(cmd *cli.Command) []string {
	var out []string
	for _, f := range cmd.Flags {
		out = append(out, f.Names()...)
	}
	return out
}

func TestSetupCommand_ClearReplacesLogout(t *testing.T) {
	t.Parallel()
	cmd := setupCommand()
	names := flagNames(cmd)

	if !slices.Contains(names, "clear") {
		t.Errorf("setup declares no --clear flag; got %v", names)
	}
	if slices.Contains(names, "logout") {
		t.Errorf("--logout still parses; the rename is breaking on purpose, so no alias survives: %v", names)
	}
	if strings.Contains(cmd.Description, "--logout") {
		t.Errorf("setup --help still advertises --logout:\n%s", cmd.Description)
	}
	if !strings.Contains(cmd.Description, "--clear") {
		t.Errorf("setup --help does not show --clear:\n%s", cmd.Description)
	}
	for _, f := range cmd.Flags {
		if slices.Contains(f.Names(), "clear") && strings.Contains(strings.ToLower(f.String()), "log out") {
			t.Errorf("--clear's usage string still talks about logging out: %s", f.String())
		}
	}
}
