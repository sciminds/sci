//go:build linux

package main

import "testing"

// TestToolsCommand_LinuxStubReturnsNil guards the build-tag wiring: the
// Linux stub must return nil so root.go's lo.Compact filters it out and
// `sci tools` doesn't show up in --help.
func TestToolsCommand_LinuxStubReturnsNil(t *testing.T) {
	if cmd := toolsCommand(); cmd != nil {
		t.Errorf("toolsCommand() = %v, want nil on Linux", cmd)
	}
}

// TestBuildRoot_LinuxOmitsTools confirms the root command tree has no
// "tools" entry on Linux — the visible symptom users would see.
func TestBuildRoot_LinuxOmitsTools(t *testing.T) {
	root := buildRoot()
	for _, c := range root.Commands {
		if c == nil {
			t.Fatal("buildRoot left a nil entry in Commands — lo.Compact wiring missing")
		}
		if c.Name == "tools" {
			t.Errorf("Linux buildRoot exposes a 'tools' command")
		}
	}
}
