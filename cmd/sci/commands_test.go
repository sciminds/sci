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
		{"py", []string{"repl", "marimo", "tutorials", "convert"}},
		{"vid", []string{"info", "mute", "strip-subs", "speed", "cut", "resize", "extract-audio", "convert", "gif", "compress"}},
		{"db", []string{"create", "reset", "info", "add", "delete", "rename"}},
		{"cloud", []string{"setup", "put", "get", "remove", "list"}},
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
	found := false
	for _, f := range root.Flags {
		for _, name := range f.Names() {
			if name == "json" {
				found = true
			}
		}
	}
	if !found {
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
	wantFlags := []string{"output", "yes", "dry-run"}
	for _, name := range wantFlags {
		found := false
		for _, f := range vid.Flags {
			for _, fn := range f.Names() {
				if fn == name {
					found = true
				}
			}
		}
		if !found {
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
		flagNames := make(map[string]bool)
		for _, f := range repl.Flags {
			for _, n := range f.Names() {
				flagNames[n] = true
			}
		}
		if !flagNames["with"] {
			t.Error("py repl should have --with flag")
		}
		if !flagNames["ignore-existing"] {
			t.Error("py repl should have --ignore-existing flag")
		}
	})

	t.Run("py_tutorials_name_all", func(t *testing.T) {
		py := findCmd(root.Commands, "py")
		tutorials := findCmd(py.Commands, "tutorials")
		if tutorials == nil {
			t.Fatal("py tutorials not found")
		}
		if len(tutorials.MutuallyExclusiveFlags) == 0 {
			t.Error("py tutorials should have mutually exclusive flags (name, all)")
		}
	})
}

// TestDoctorCommand checks the doctor command is registered.
func TestDoctorCommand(t *testing.T) {
	root := buildRoot()
	doc := findCmd(root.Commands, "doctor")
	if doc == nil {
		t.Fatal("doctor not found")
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

// TestHelpOutput verifies help renders without panics.
func TestHelpOutput(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf
	_ = root.Run(context.Background(), []string{"sci", "--help"})
	out := buf.String()
	if !strings.Contains(out, "sci") {
		t.Errorf("help should mention 'sci', got:\n%s", out)
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
