package brew

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSnapshot_HappyPath(t *testing.T) {
	t.Parallel()
	m := &mockRunner{
		leavesResult:       []string{"git", "curl"},
		listFormulaeResult: []string{"git", "curl", "sqlite"},
		listCasksResult:    []string{"firefox", "zed"},
		tapsResult:         []string{"oven-sh/bun"},
		uvToolListResult:   []string{"marimo", "ruff"},
	}

	snap, err := CollectSnapshot(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.Leaves) != 2 {
		t.Errorf("Leaves = %v, want 2 items", snap.Leaves)
	}
	if len(snap.Formulae) != 3 {
		t.Errorf("Formulae = %v, want 3 items", snap.Formulae)
	}
	if len(snap.Casks) != 2 {
		t.Errorf("Casks = %v, want 2 items", snap.Casks)
	}
	if len(snap.Taps) != 1 {
		t.Errorf("Taps = %v, want 1 item", snap.Taps)
	}
	if len(snap.UVTools) != 2 {
		t.Errorf("UVTools = %v, want 2 items", snap.UVTools)
	}
}

func TestCollectSnapshot_LeavesError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{leavesErr: errors.New("leaves failed")}

	_, err := CollectSnapshot(m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCollectSnapshot_FormulaeError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{listFormulaeErr: errors.New("list failed")}

	_, err := CollectSnapshot(m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCollectSnapshot_UVError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{uvToolListErr: errors.New("uv failed")}

	_, err := CollectSnapshot(m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSystemSnapshot_IsInstalled(t *testing.T) {
	t.Parallel()
	snap := SystemSnapshot{
		Leaves:   []string{"git", "curl"},
		Formulae: []string{"git", "curl", "sqlite", "oven-sh/bun/bun"},
		Casks:    []string{"firefox", "zed"},
		Taps:     []string{"oven-sh/bun"},
		UVTools:  []string{"marimo", "ruff"},
	}

	tests := []struct {
		typ, name string
		want      bool
	}{
		// brew formulae — checks Formulae list (not just Leaves)
		{"brew", "git", true},
		{"brew", "sqlite", true},          // dep-only, not in Leaves
		{"brew", "oven-sh/bun/bun", true}, // tap-qualified
		{"brew", "node", false},           // not installed
		// casks
		{"cask", "firefox", true},
		{"cask", "slack", false},
		// taps
		{"tap", "oven-sh/bun", true},
		{"tap", "homebrew/core", false},
		// uv tools
		{"uv", "marimo", true},
		{"uv", "symbex", false},
		// unknown type
		{"cargo", "ripgrep", false},
	}

	for _, tt := range tests {
		t.Run(tt.typ+"/"+tt.name, func(t *testing.T) {
			got := snap.IsInstalled(tt.typ, tt.name)
			if got != tt.want {
				t.Errorf("IsInstalled(%q, %q) = %v, want %v", tt.typ, tt.name, got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in, want string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute/path", "/absolute/path"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		got, err := ExpandPath(tt.in)
		if err != nil {
			t.Fatalf("ExpandPath(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
