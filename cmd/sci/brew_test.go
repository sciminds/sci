package main

import (
	"testing"
)

func TestBrewCommandTree(t *testing.T) {
	root := buildRoot()
	brew := findCmd(root.Commands, "brew")
	if brew == nil {
		t.Fatal("brew command not found")
	}

	if brew.Category != "Maintenance" {
		t.Errorf("brew category = %q, want %q", brew.Category, "Maintenance")
	}

	children := []string{"install", "uninstall", "list"}
	for _, name := range children {
		if findCmd(brew.Commands, name) == nil {
			t.Errorf("brew: missing child command %q", name)
		}
	}
}

func TestBrewFileFlag(t *testing.T) {
	root := buildRoot()
	brew := findCmd(root.Commands, "brew")
	if brew == nil {
		t.Fatal("brew command not found")
	}
	found := false
	for _, f := range brew.Flags {
		for _, name := range f.Names() {
			if name == "file" {
				found = true
			}
		}
	}
	if !found {
		t.Error("brew should have a --file flag")
	}
}

func TestBrewInstallFlags(t *testing.T) {
	root := buildRoot()
	brew := findCmd(root.Commands, "brew")
	install := findCmd(brew.Commands, "install")
	if install == nil {
		t.Fatal("brew install not found")
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
			t.Errorf("brew install should have flag --%s", name)
		}
	}
}

func TestBrewListFlags(t *testing.T) {
	root := buildRoot()
	brew := findCmd(root.Commands, "brew")
	list := findCmd(brew.Commands, "list")
	if list == nil {
		t.Fatal("brew list not found")
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
			t.Errorf("brew list should have flag --%s", name)
		}
	}
}
