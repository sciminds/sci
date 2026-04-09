package main

import (
	"testing"
)

func TestToolsCommandTree(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	if tools == nil {
		t.Fatal("tools command not found")
	}

	if tools.Category != "Maintenance" {
		t.Errorf("tools category = %q, want %q", tools.Category, "Maintenance")
	}

	children := []string{"install", "uninstall", "list", "update", "reccs"}
	for _, name := range children {
		if findCmd(tools.Commands, name) == nil {
			t.Errorf("tools: missing child command %q", name)
		}
	}
}

func TestToolsFileFlag(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	if tools == nil {
		t.Fatal("tools command not found")
	}
	found := false
	for _, f := range tools.Flags {
		for _, name := range f.Names() {
			if name == "file" {
				found = true
			}
		}
	}
	if !found {
		t.Error("tools should have a --file flag")
	}
}

func TestToolsInstallFlags(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	install := findCmd(tools.Commands, "install")
	if install == nil {
		t.Fatal("tools install not found")
	}

	wantFlags := []string{"cask", "tap", "uv", "go", "cargo"}
	for _, name := range wantFlags {
		found := false
		for _, f := range install.Flags {
			for _, fn := range f.Names() {
				if fn == name {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("tools install should have flag --%s", name)
		}
	}
}

func TestToolsListFlags(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	list := findCmd(tools.Commands, "list")
	if list == nil {
		t.Fatal("tools list not found")
	}

	wantFlags := []string{"formula", "cask", "tap", "uv", "go", "cargo", "all"}
	for _, name := range wantFlags {
		found := false
		for _, f := range list.Flags {
			for _, fn := range f.Names() {
				if fn == name {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("tools list should have flag --%s", name)
		}
	}
}

func TestDoctorIsLeafCommand(t *testing.T) {
	root := buildRoot()
	doc := findCmd(root.Commands, "doctor")
	if doc == nil {
		t.Fatal("doctor command not found")
	}
	if len(doc.Commands) > 0 {
		t.Errorf("doctor should be a leaf command (no subcommands), got %d", len(doc.Commands))
	}
	if doc.Action == nil {
		t.Error("doctor should have an Action")
	}
}
