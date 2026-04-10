package main

import (
	"testing"
)

func TestToolsFileFlag(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	if tools == nil {
		t.Fatal("tools command not found")
	}
	if !hasFlag(tools, "file") {
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

	for _, name := range []string{"cask", "tap", "uv", "go", "cargo"} {
		if !hasFlag(install, name) {
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

	for _, name := range []string{"formula", "cask", "tap", "uv", "go", "cargo", "all"} {
		if !hasFlag(list, name) {
			t.Errorf("tools list should have flag --%s", name)
		}
	}
}
