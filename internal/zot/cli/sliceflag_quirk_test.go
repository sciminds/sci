package cli

import (
	"context"
	"slices"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestSliceFlagLocalQuirk_Reproduction documents the urfave/cli v3 bug we
// work around throughout this package, and locks the workaround in place.
//
// The bug: any SliceFlag with `Local: true` keeps only the LAST --flag
// occurrence on the command line. When `Local: true` is set, urfave/cli
// re-runs PreParse on every Set call, and SliceBase.Create zeroes the
// underlying slice each time — so accumulated values are wiped before the
// new one is appended.
//
// The fix: drop `Local: true` for slice flags. (Destination is fine; the
// trigger is Local, not Destination.) The flag still doesn't propagate to
// children in practice because every slice-flag site here is on a leaf
// command. Marker comment is `// lint:no-local` to suppress lint-guard
// rule 4.
//
// If this test ever starts failing in either direction, urfave/cli has
// changed behavior — re-audit every `// lint:no-local` site to see whether
// the workaround is still needed.
func TestSliceFlagLocalQuirk_Reproduction(t *testing.T) {
	t.Parallel()
	type scenario struct {
		name        string
		flag        func(dest *[]string) cli.Flag
		want        []string
		wantViaPeek []string // what cmd.StringSlice() returns
	}
	scenarios := []scenario{
		{
			name: "destination_NOT_local",
			flag: func(dest *[]string) cli.Flag {
				return &cli.StringSliceFlag{Name: "x", Destination: dest}
			},
			want:        []string{"a", "b", "c"},
			wantViaPeek: []string{"a", "b", "c"},
		},
		{
			name: "destination_AND_local_BUG",
			flag: func(dest *[]string) cli.Flag {
				return &cli.StringSliceFlag{Name: "x", Destination: dest, Local: true}
			},
			want:        []string{"c"}, // BUG: only last value
			wantViaPeek: []string{"c"}, // peek is also broken under Local
		},
		{
			name: "no_destination_NOT_local",
			flag: func(_ *[]string) cli.Flag {
				return &cli.StringSliceFlag{Name: "x"}
			},
			want:        nil,
			wantViaPeek: []string{"a", "b", "c"},
		},
		{
			name: "no_destination_AND_local_BUG",
			flag: func(_ *[]string) cli.Flag {
				return &cli.StringSliceFlag{Name: "x", Local: true}
			},
			want:        nil,
			wantViaPeek: []string{"c"}, // BUG: only last value
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			var dest []string
			var peek []string
			cmd := &cli.Command{
				Name:  "x",
				Flags: []cli.Flag{sc.flag(&dest)},
				Action: func(_ context.Context, cmd *cli.Command) error {
					peek = cmd.StringSlice("x")
					return nil
				},
			}
			err := cmd.Run(context.Background(), []string{"x", "--x", "a", "--x", "b", "--x", "c"})
			if err != nil {
				t.Fatal(err)
			}
			if !slicesEqual(dest, sc.want) {
				t.Errorf("dest = %v, want %v", dest, sc.want)
			}
			if !slicesEqual(peek, sc.wantViaPeek) {
				t.Errorf("cmd.StringSlice = %v, want %v", peek, sc.wantViaPeek)
			}
		})
	}
}

// TestSliceFlagFix_AllProductionFlagsAccumulate runs every slice flag we
// expose to users with three repeated occurrences and asserts the values
// all reach the destination. Catches regressions if anyone re-adds
// `Local: true` to a slice flag.
func TestSliceFlagFix_AllProductionFlagsAccumulate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		// argv is the command line minus the leading binary name.
		argv []string
		// check fires after the command runs and inspects the state the
		// flag was supposed to populate. The action it lives under may
		// fail (no DB / no network), but flag parsing happens before
		// the action runs, so we capture state via cmd.StringSlice from
		// inside a Before hook that also short-circuits the action.
		flagName string
	}{
		{name: "item add --tag", argv: []string{"item", "add", "--title", "x", "--tag", "a", "--tag", "b", "--tag", "c"}, flagName: "tag"},
		// Author values must be comma-free here — urfave/cli's default
		// slice separator is comma, so "Smith, A" would split. Use
		// single-name creators ("Alice", "Bob") to isolate the Local
		// bug from the orthogonal comma-split behavior.
		{name: "item add --author", argv: []string{"item", "add", "--title", "x", "--author", "Alice", "--author", "Bob", "--author", "Carol"}, flagName: "author"},
		{name: "item note add --tag", argv: []string{"item", "note", "add", "PARENT12", "--body", "x", "--tag", "a", "--tag", "b", "--tag", "c"}, flagName: "tag"},
		{name: "find works --filter", argv: []string{"find", "works", "--filter", "k1=v1", "--filter", "k2=v2", "--filter", "k3=v3", "q"}, flagName: "filter"},
		{name: "llm query --key", argv: []string{"llm", "query", "--key", "ABC12345", "--key", "DEF67890", "--key", "GHI34567", "--", "select 1"}, flagName: "key"},
		{name: "doctor citekeys --kind", argv: []string{"doctor", "citekeys", "--fix", "--kind", "invalid", "--kind", "collision", "--kind", "non-canonical"}, flagName: "kind"},
		{name: "doctor citekeys --item", argv: []string{"doctor", "citekeys", "--fix", "--item", "AAAA1111", "--item", "BBBB2222", "--item", "CCCC3333"}, flagName: "item"},
		{name: "saved-search create --condition", argv: []string{"saved-search", "create", "x", "--condition", "title:contains:a", "--condition", "tag:is:b", "--condition", "itemType:is:c"}, flagName: "condition"},
		{name: "saved-search update --condition", argv: []string{"saved-search", "update", "ABCD1234", "--condition", "title:contains:a", "--condition", "tag:is:b", "--condition", "itemType:is:c"}, flagName: "condition"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			// Build a shadow tree mirroring the production root, but with
			// every leaf command's Action replaced by one that captures
			// the flag we care about and exits cleanly. We do this by
			// wrapping the production Commands() and rewriting the
			// matching leaf in-place.
			root := &cli.Command{
				Name:     "zot",
				Flags:    PersistentFlags(),
				Commands: shadowCommands(t, tc.argv, tc.flagName, &captured),
				// Library validation deliberately not installed — the
				// shadow leaf actions don't need it, and skipping the
				// Before hook keeps the test independent of config.
			}
			argv := slices.Concat([]string{"zot", "--library", "personal"}, tc.argv)
			if err := root.Run(context.Background(), argv); err != nil {
				t.Fatalf("run: %v", err)
			}
			want := lastNValues(tc.argv, tc.flagName, 3)
			if len(want) != 3 {
				t.Fatalf("test bug: extracted %d values, want 3 (%v)", len(want), want)
			}
			if !slicesEqual(captured, want) {
				t.Errorf("captured = %v, want %v", captured, want)
			}
		})
	}
}

// shadowCommands returns the production command tree, but with the leaf
// command targeted by argv rewritten to capture flag state and stop. Walking
// the tree allows the test to exercise the real flag definitions (and so
// reproduce the Local: true bug) without depending on each command's
// Before/Action environment (DB, network, etc.).
func shadowCommands(t *testing.T, argv []string, flagName string, out *[]string) []*cli.Command {
	t.Helper()
	cmds := Commands()
	leaf := walkToLeaf(cmds, argv)
	if leaf == nil {
		t.Fatalf("could not locate leaf command for argv %v", argv)
	}
	leaf.Action = func(_ context.Context, cmd *cli.Command) error {
		*out = slices.Clone(cmd.StringSlice(flagName))
		return nil
	}
	return cmds
}

// walkToLeaf walks the command tree following the non-flag tokens in argv
// (positional command names) and returns the deepest command located.
// Returns nil if any segment is unmatched.
func walkToLeaf(cmds []*cli.Command, argv []string) *cli.Command {
	var current *cli.Command
	for _, tok := range argv {
		if len(tok) > 0 && tok[0] == '-' {
			break // hit the flag tail; we've reached the leaf
		}
		var next *cli.Command
		for _, c := range cmds {
			if c.Name == tok {
				next = c
				break
			}
			for _, a := range c.Aliases {
				if a == tok {
					next = c
					break
				}
			}
			if next != nil {
				break
			}
		}
		if next == nil {
			// Token is a positional argument to the current leaf, not a
			// subcommand name — we're done walking.
			return current
		}
		current = next
		cmds = next.Commands
	}
	return current
}

// lastNValues extracts the last n values supplied for --flagName in argv.
// Used by the multi-flag accumulation test as the ground-truth expectation.
func lastNValues(argv []string, flagName string, n int) []string {
	target := "--" + flagName
	var seen []string
	for i := 0; i < len(argv); i++ {
		if argv[i] == target && i+1 < len(argv) {
			seen = append(seen, argv[i+1])
			i++
		}
	}
	if len(seen) <= n {
		return seen
	}
	return seen[len(seen)-n:]
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
