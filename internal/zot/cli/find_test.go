package cli

import (
	"slices"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestFindFlags_LimitAliases asserts that the canonical --limit flag keeps
// --per-page as a back-compat alias (older scripts) and -n as shorthand
// (mirrors --limit on search / item list).
func TestFindFlags_LimitAliases(t *testing.T) {
	t.Parallel()
	flags := findFlags()
	var limit *cli.IntFlag
	for _, f := range flags {
		if intf, ok := f.(*cli.IntFlag); ok && intf.Name == "limit" {
			limit = intf
			break
		}
	}
	if limit == nil {
		t.Fatal("findFlags() has no --limit flag")
	}
	names := slices.Concat([]string{limit.Name}, limit.Aliases)
	for _, want := range []string{"limit", "per-page", "n"} {
		if !slices.Contains(names, want) {
			t.Errorf("--limit flag missing alias %q (have %v)", want, names)
		}
	}
}
