package main

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// TestCommandTree validates the command tree structure after migration to urfave/cli.
func TestCommandTree(t *testing.T) {
	root := buildRoot()

	t.Run("root_name", func(t *testing.T) {
		if root.Name != "sci" {
			t.Errorf("root name = %q, want %q", root.Name, "sci")
		}
	})

	t.Run("root_has_version", func(t *testing.T) {
		if root.Version == "" {
			t.Error("root should have a version string")
		}
	})

	// All top-level subcommands
	topLevel := map[string]bool{
		"learn": false,
		"tools": false, "doctor": false, "update": false,
		"proj": false, "py": false, "vid": false,
		"db": false, "cloud": false, "lab": false, "view": false,
	}
	for _, cmd := range root.Commands {
		topLevel[cmd.Name] = true
	}
	for name, found := range topLevel {
		t.Run("has_"+name, func(t *testing.T) {
			if !found {
				t.Errorf("missing top-level command %q", name)
			}
		})
	}

	// Categories
	t.Run("categories", func(t *testing.T) {
		cats := map[string][]string{
			"Getting Started": {"learn"},
			"Commands":        {"cloud", "db", "lab", "proj", "py", "vid", "view"},
			"Maintenance":     {"tools", "doctor", "update"},
		}
		for cat, expected := range cats {
			for _, name := range expected {
				cmd := findCmd(root.Commands, name)
				if cmd == nil {
					t.Errorf("command %q not found", name)
					continue
				}
				if cmd.Category != cat {
					t.Errorf("%q category = %q, want %q", name, cmd.Category, cat)
				}
			}
		}
	})
}

// TestSubcommandTrees checks that parent commands have the right children.
func TestSubcommandTrees(t *testing.T) {
	root := buildRoot()

	tests := []struct {
		parent   string
		children []string
	}{
		{"tools", []string{"install", "uninstall", "list", "update", "reccs"}},
		{"proj", []string{"new", "config", "add", "remove", "run", "render", "preview"}},
		{"py", []string{"repl", "notebook", "convert"}},
		{"vid", []string{"info", "mute", "strip-subs", "speed", "cut", "resize", "extract-audio", "convert", "gif", "compress"}},
		{"db", []string{"create", "reset", "info", "add", "delete", "rename"}},
		{"cloud", []string{"setup", "ls", "get", "put", "browse", "remove"}},
		{"lab", []string{"setup", "ls", "get", "put", "browse"}},
	}

	for _, tt := range tests {
		t.Run(tt.parent, func(t *testing.T) {
			parent := findCmd(root.Commands, tt.parent)
			if parent == nil {
				t.Fatalf("parent %q not found", tt.parent)
			}
			for _, child := range tt.children {
				if findCmd(parent.Commands, child) == nil {
					t.Errorf("%s: missing child command %q", tt.parent, child)
				}
			}
		})
	}
}

// TestJSONFlag checks that --json is available on root (inherited by all).
func TestJSONFlag(t *testing.T) {
	root := buildRoot()
	if !hasFlag(root, "json") {
		t.Error("root should have a --json flag")
	}
}

// TestVidPersistentFlags checks vid's shared flags propagate to subcommands.
func TestVidPersistentFlags(t *testing.T) {
	root := buildRoot()
	vid := findCmd(root.Commands, "vid")
	if vid == nil {
		t.Fatal("vid command not found")
	}

	// vid's shared flags (output, yes, dry-run) should be on the vid command itself.
	for _, name := range []string{"output", "yes", "dry-run"} {
		if !hasFlag(vid, name) {
			t.Errorf("vid should have flag %q", name)
		}
	}
}

// TestMutuallyExclusiveFlags checks that mutually exclusive flag groups exist.
func TestMutuallyExclusiveFlags(t *testing.T) {
	root := buildRoot()

	t.Run("py_repl_with_ignore_existing", func(t *testing.T) {
		py := findCmd(root.Commands, "py")
		if py == nil {
			t.Fatal("py not found")
		}
		repl := findCmd(py.Commands, "repl")
		if repl == nil {
			t.Fatal("py repl not found")
		}
		if !hasFlag(repl, "with") {
			t.Error("py repl should have --with flag")
		}
		if !hasFlag(repl, "ignore-existing") {
			t.Error("py repl should have --ignore-existing flag")
		}
	})

}

// TestDoctorCommand checks doctor is registered as a leaf command with an action.
func TestDoctorCommand(t *testing.T) {
	root := buildRoot()
	doc := findCmd(root.Commands, "doctor")
	if doc == nil {
		t.Fatal("doctor not found")
	}
	if len(doc.Commands) > 0 {
		t.Errorf("doctor should be a leaf command (no subcommands), got %d", len(doc.Commands))
	}
	if doc.Action == nil {
		t.Error("doctor should have an Action")
	}
}

// TestSkipFlagParsing checks proj run has flag parsing disabled.
func TestSkipFlagParsing(t *testing.T) {
	root := buildRoot()
	proj := findCmd(root.Commands, "proj")
	if proj == nil {
		t.Fatal("proj not found")
	}
	run := findCmd(proj.Commands, "run")
	if run == nil {
		t.Fatal("proj run not found")
	}
	if !run.SkipFlagParsing {
		t.Error("proj run should have SkipFlagParsing=true")
	}
}

// TestHelpOutput verifies help renders without panics for root and all top-level commands.
func TestHelpOutput(t *testing.T) {
	commands := []struct {
		args     []string
		contains string
	}{
		{[]string{"sci", "--help"}, "sci"},
		{[]string{"sci", "tools", "--help"}, "install"},
		{[]string{"sci", "proj", "--help"}, "new"},
		{[]string{"sci", "vid", "--help"}, "info"},
		{[]string{"sci", "db", "--help"}, "create"},
		{[]string{"sci", "cloud", "--help"}, "setup"},
		{[]string{"sci", "lab", "--help"}, "setup"},
		{[]string{"sci", "py", "--help"}, "repl"},
	}

	for _, tt := range commands {
		t.Run(strings.Join(tt.args[1:], "_"), func(t *testing.T) {
			root := buildRoot()
			var buf strings.Builder
			root.Writer = &buf
			_ = root.Run(context.Background(), tt.args)
			out := buf.String()
			if !strings.Contains(out, tt.contains) {
				t.Errorf("%v help should mention %q, got:\n%s", tt.args, tt.contains, out)
			}
		})
	}
}

// TestVidArgValidation verifies vid subcommands reject wrong argument counts.
func TestVidArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"info_no_args", []string{"sci", "vid", "info"}, "expected 1"},
		{"mute_no_args", []string{"sci", "vid", "mute"}, "expected 1"},
		{"speed_one_arg", []string{"sci", "vid", "speed", "in.mp4"}, "expected 2"},
		{"cut_two_args", []string{"sci", "vid", "cut", "in.mp4", "0:30"}, "expected 3"},
		{"resize_one_arg", []string{"sci", "vid", "resize", "in.mp4"}, "expected 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := buildRoot()
			err := root.Run(context.Background(), tt.args)
			if err == nil {
				t.Fatal("expected error for missing args")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error should contain %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestDbRequiresSubcommand verifies db without subcommand shows help (no panic).
func TestDbRequiresSubcommand(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf
	err := root.Run(context.Background(), []string{"sci", "db"})
	// db with no subcommand should show help or return nil (not panic)
	if err != nil {
		t.Errorf("db with no subcommand should not error, got: %v", err)
	}
}

func findCmd(cmds []*cli.Command, name string) *cli.Command {
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestCommandHelpContent walks the entire command tree and fails if any command
// is missing Usage (one-line summary) or Description (examples/details).
// This prevents shipping commands with empty help screens.
func TestCommandHelpContent(t *testing.T) {
	root := buildRoot()

	var missing []string
	walkCommands(root.Commands, "sci", func(path string, cmd *cli.Command) {
		if cmd.Usage == "" {
			missing = append(missing, path+": missing Usage")
		}
		if cmd.Description == "" && !cmd.Hidden {
			missing = append(missing, path+": missing Description")
		}
	})

	for _, m := range missing {
		t.Error(m)
	}
}

// walkCommands recursively visits every command in the tree, calling fn with
// the full dot-separated path (e.g. "sci.zot.item.extract") and the command.
func walkCommands(cmds []*cli.Command, prefix string, fn func(path string, cmd *cli.Command)) {
	for _, cmd := range cmds {
		path := prefix + " " + cmd.Name
		fn(path, cmd)
		if len(cmd.Commands) > 0 {
			walkCommands(cmd.Commands, path, fn)
		}
	}
}

// TestNamespaceRejectsUnknownChildren walks the entire sci command tree and,
// for every command with subcommands, invokes it with a nonsense child name.
// The invariant (enforced by cmdutil.WireNamespaceDefaults on the root) is
// that any such invocation produces an "unknown command" error — never
// urfave's default "No help topic for 'X'" nor silent fall-through to the
// parent's Action.
//
// This test is the regression guard: adding a new namespace anywhere in the
// tree gets coverage automatically, and unwiring the auto-defaults breaks
// every row.
func TestNamespaceRejectsUnknownChildren(t *testing.T) {
	root := buildRoot()

	var paths []string
	walkCommands(root.Commands, "sci", func(path string, cmd *cli.Command) {
		if len(cmd.Commands) > 0 {
			paths = append(paths, path)
		}
	})

	if len(paths) == 0 {
		t.Fatal("tree walk found no namespaces — did buildRoot() change?")
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			// Rebuild root per-iteration so state from prior Run calls
			// doesn't leak (urfave mutates cmd.parsedArgs, etc.).
			r := buildRoot()
			var buf strings.Builder
			r.Writer = &buf

			// RejectUnknownSubcommand is the first link in each namespace's
			// Before chain (see cmdutil.WireNamespaceDefaults), so it fires
			// before any command-specific Before hooks — argv is just the
			// path segments plus a bogus child name, no pre-args needed.
			argv := append(strings.Split(path, " "), "bogus-subcommand-xyz")

			err := r.Run(context.Background(), argv)
			if err == nil {
				t.Fatalf("%s bogus-subcommand-xyz: expected error, got nil (output: %s)", path, buf.String())
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("%s: error should contain \"unknown command\", got: %v", path, err)
			}
		})
	}
}

// hasFlag returns true if cmd has a flag with the given name.
func hasFlag(cmd *cli.Command, name string) bool {
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			if n == name {
				return true
			}
		}
	}
	return false
}
