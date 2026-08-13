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
//
// Subtests run serially (no t.Parallel inside the loop): every Commands()
// build re-binds the same package-level flag state, and urfave/cli's
// PreParse re-zeroes it on every Run — so parallel subtests race on shared
// memory and intermittently capture truncated or empty slices. Production
// never hits this because each user invocation is a fresh process.
// -race flags it instantly.
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
		// doctor --check was missing from this table and carried
		// Local: true, so `--check invalid --check missing` ran ONLY
		// missing — a diagnostic quietly doing less than it was asked
		// while its report looked complete.
		{name: "doctor --check", argv: []string{"doctor", "--check", "invalid", "--check", "missing", "--check", "orphans"}, flagName: "check"},
	}

	for _, tc := range cases {
		// NB: no t.Parallel() — see test comment above. Subtests share
		// package-level slice-flag Destinations and PreParse races them.
		t.Run(tc.name, func(t *testing.T) {
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

// TestSliceFlagSeparator_Reproduction pins the OTHER slice-flag trap,
// which is orthogonal to the Local bug above and was worse.
//
// urfave/cli splits slice values on comma by default, so a free-text value
// arrives in pieces. `--author "Smith, Alice"` became ["Smith", " Alice"],
// and parseCreator read each comma-less half as an INSTITUTIONAL name —
// one author silently became two organizations. `--condition
// "title:contains:Cambridge, MA"` split into a valid spec plus the
// unparseable fragment " MA", an error naming something the user never
// typed. The cure is DisableSliceFlagSeparator, which is a *command*-level
// setting: no per-flag form, and no inheritance from the root, so each
// command opts in on its own.
//
// The reproduction is synthetic because no surviving zot command carries a
// free-text slice flag today — `doctor --check` takes enum names and wants
// the comma split. That is exactly why this test is worth keeping: the next
// person to add one needs the trap written down and gated, not rediscovered
// on a live library. Extend it into a production table the moment such a
// flag lands.
func TestSliceFlagSeparator_Reproduction(t *testing.T) {
	t.Parallel()
	const commaBearing = "Smith, Alice"

	for _, tc := range []struct {
		name    string
		disable bool
		want    []string
	}{
		{name: "default splits on comma", disable: false, want: []string{"Smith", " Alice"}},
		{name: "DisableSliceFlagSeparator keeps it whole", disable: true, want: []string{commaBearing}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			root := &cli.Command{
				Name: "zot",
				Commands: []*cli.Command{{
					Name:                      "demo",
					DisableSliceFlagSeparator: tc.disable,
					Flags: []cli.Flag{
						// lint:no-local — slice-flag Local quirk, see above.
						&cli.StringSliceFlag{Name: "author"},
					},
					Action: func(_ context.Context, cmd *cli.Command) error {
						captured = slices.Clone(cmd.StringSlice("author"))
						return nil
					},
				}},
			}
			if err := root.Run(context.Background(), []string{"zot", "demo", "--author", commaBearing}); err != nil {
				t.Fatalf("run: %v", err)
			}
			if !slicesEqual(captured, tc.want) {
				t.Errorf("captured = %q, want %q", captured, tc.want)
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
			if slices.Contains(c.Aliases, tok) {
				next = c
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
