//go:build darwin

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

	for _, name := range []string{"formula", "cask", "tap", "uv", "go", "cargo"} {
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

func TestToolsReccsAppsFlag(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	reccs := findCmd(tools.Commands, "reccs")
	if reccs == nil {
		t.Fatal("tools reccs not found")
	}
	for _, name := range []string{"install", "all", "include", "exclude", "dry-run", "apps"} {
		if !hasFlag(reccs, name) {
			t.Errorf("tools reccs should have flag --%s", name)
		}
	}
}

func TestToolsAppsCommand(t *testing.T) {
	root := buildRoot()
	tools := findCmd(root.Commands, "tools")
	apps := findCmd(tools.Commands, "apps")
	if apps == nil {
		t.Fatal("tools apps command not found")
	}
	for _, name := range []string{"install", "all", "include", "exclude", "dry-run"} {
		if !hasFlag(apps, name) {
			t.Errorf("tools apps should have flag --%s", name)
		}
	}
	// `apps` is pre-scoped to casks, so a redundant --apps flag would be confusing.
	if hasFlag(apps, "apps") {
		t.Error("tools apps should not expose a redundant --apps flag")
	}
}
