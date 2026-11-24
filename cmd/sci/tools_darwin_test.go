//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/brew"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/sciconfig"
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

const ignoredLibreOffice = `{"ignored":[{"type":"cask","name":"libreoffice"}]}`

// writeToolsConfig makes content the tools.json this test reads.
func writeToolsConfig(t *testing.T, content string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	path := sciconfig.Path("tools.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runToolsCapturingStdout runs `sci args...` and returns what it wrote to stdout.
func runToolsCapturingStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	runErr := buildRoot().Run(context.Background(), slices.Concat([]string{"sci"}, args))
	_ = writer.Close()
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func TestToolsIgnoreFlags(t *testing.T) {
	tools := findCmd(buildRoot().Commands, "tools")
	for _, name := range []string{"ignore", "unignore"} {
		cmd := findCmd(tools.Commands, name)
		if cmd == nil {
			t.Errorf("tools %s not found", name)
			continue
		}
		for _, flag := range []string{"formula", "cask", "uv"} {
			if !hasFlag(cmd, flag) {
				t.Errorf("tools %s should have flag --%s", name, flag)
			}
		}
		for _, flag := range []string{"tap", "go", "cargo"} {
			if hasFlag(cmd, flag) {
				t.Errorf("tools %s should not have flag --%s", name, flag)
			}
		}
	}
}

func TestToolsIgnore_ListsWithoutNetwork(t *testing.T) {
	writeToolsConfig(t, ignoredLibreOffice)
	out, err := runToolsCapturingStdout(t, "tools", "ignore", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data brew.IgnoreListResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("stdout %q: %v", out, err)
	}
	if !envelope.Data.Ignored.Contains(brew.TypeCask, "libreoffice") {
		t.Errorf("stdout = %s, want the libreoffice cask", out)
	}
}

func TestToolsUnignore_RequiresName(t *testing.T) {
	writeToolsConfig(t, ignoredLibreOffice)
	_, err := runToolsCapturingStdout(t, "tools", "unignore", "--json")
	var coded *cmdutil.CodedError
	if !errors.As(err, &coded) || coded.Code != cmdutil.CodeUsage || !strings.Contains(err.Error(), "package name") {
		t.Errorf("err = %v, want a usage error asking for a package name", err)
	}
}

func TestToolsUnignore_DryRun(t *testing.T) {
	path := writeToolsConfig(t, ignoredLibreOffice)
	if _, err := runToolsCapturingStdout(t, "tools", "--dry-run", "unignore", "libreoffice"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != ignoredLibreOffice {
		t.Errorf("tools.json = %s, want it unchanged", data)
	}
}

func TestToolsIgnore_HelpStatesDependencyLimit(t *testing.T) {
	ignore := findCmd(findCmd(buildRoot().Commands, "tools").Commands, "ignore")
	if ignore == nil {
		t.Fatal("tools ignore not found")
	}
	if !strings.Contains(ignore.Description, "dependency") {
		t.Errorf("description = %q, want it to mention dependency upgrades", ignore.Description)
	}
}
