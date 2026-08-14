package main

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestVidFlagsReachSubcommand guards the flag-reading contract every `sci vid`
// runner depends on: the shared --output / --yes / --dry-run flags live on the
// `vid` parent and propagate down, while each verb's own flags (and their
// Value: defaults) resolve on the subcommand itself. All of them are read via
// cmd.<Type>("name") rather than a package-level destination, so a lineage or
// default-resolution regression would silently change behaviour.
func TestVidFlagsReachSubcommand(t *testing.T) {
	root := buildRoot()
	gif := findCmd(findCmd(root.Commands, "vid").Commands, "gif")
	if gif == nil {
		t.Fatal("vid gif command not found")
	}

	var ran bool
	gif.Action = func(_ context.Context, cmd *cli.Command) error {
		ran = true
		if got := cmd.String("output"); got != "/tmp/out.gif" {
			t.Errorf("shared --output: got %q, want /tmp/out.gif", got)
		}
		if !cmd.Bool("dry-run") {
			t.Error("shared --dry-run: not visible from the subcommand")
		}
		if !cmd.Bool("yes") {
			t.Error("shared --yes: not visible from the subcommand")
		}
		if got := cmd.String("start"); got != "0:05" {
			t.Errorf("local --start: got %q, want 0:05", got)
		}
		// Unset local flags must still report their declared defaults.
		if got := cmd.Int("width"); got != 480 {
			t.Errorf("local --width default: got %d, want 480", got)
		}
		if got := cmd.Int("fps"); got != 12 {
			t.Errorf("local --fps default: got %d, want 12", got)
		}
		return nil
	}

	err := root.Run(context.Background(), []string{
		"sci", "vid", "--output", "/tmp/out.gif", "--dry-run", "--yes",
		"gif", "demo.mov", "--start", "0:05",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !ran {
		t.Fatal("vid gif action never ran")
	}
}
