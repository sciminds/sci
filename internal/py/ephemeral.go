// Package py launches Python environments via uv or pixi.
//
// These sessions auto-detect the current environment (pixi, uv project,
// or ephemeral) and replace the current process (via syscall.Exec) with
// the requested Python tool.
//
// Key functions:
//
//   - [RunTool] detects the environment and launches a tool (IPython, marimo, etc.)
//   - [DetectEnv] checks for pixi / uv environments in a directory
//   - [BuildUVArgs] / [BuildPixiCmd] construct command-line arguments (testable)
//
// Predefined tools: [IPythonTool], [MarimoTool].
package py

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sciminds/cli/internal/ui"
)

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

// Tool describes a Python tool that can be launched in any environment.
type Tool struct {
	Pkg         string   // package name for uv --with (e.g. "ipython", "marimo")
	UVRun       []string // command after "--" for uv (e.g. ["ipython"], ["marimo", "edit"])
	PixiModule  string   // python -m <module> for pixi (e.g. "IPython", "marimo")
	PixiArgs    []string // extra args after -m <module> for pixi (e.g. nil, ["edit"])
	DefaultPkgs []string // additional packages for ephemeral sessions
	Label       string   // human-readable name for status messages
}

// IPythonTool launches an IPython REPL with common data-science packages.
var IPythonTool = Tool{
	Pkg:        "ipython",
	UVRun:      []string{"ipython"},
	PixiModule: "IPython",
	DefaultPkgs: []string{
		"numpy",
		"scipy",
		"seaborn",
		"polars",
		"scikit-learn",
		"bossanova",
	},
	Label: "IPython",
}

// MarimoTool launches a marimo notebook editor.
var MarimoTool = Tool{
	Pkg:        "marimo",
	UVRun:      []string{"marimo", "edit"},
	PixiModule: "marimo",
	PixiArgs:   []string{"edit"},
	Label:      "marimo",
}

// ---------------------------------------------------------------------------
// Environment detection
// ---------------------------------------------------------------------------

// EnvKind describes which Python environment was detected.
type EnvKind int

const (
	EnvNone EnvKind = iota // no environment found
	EnvPixi                // .pixi directory present
	EnvUV                  // pyproject.toml + .venv present
)

// EnvInfo holds the detected environment kind and its root directory.
type EnvInfo struct {
	Kind EnvKind
	Dir  string
}

// DetectEnv checks dir for an existing Python environment.
// Pixi takes priority over uv since it manages its own envs.
func DetectEnv(dir string) EnvInfo {
	// Pixi takes priority — it manages its own envs.
	if isDir(filepath.Join(dir, ".pixi")) {
		return EnvInfo{Kind: EnvPixi, Dir: dir}
	}

	// uv/venv: needs both pyproject.toml and .venv.
	if isFile(filepath.Join(dir, "pyproject.toml")) && isDir(filepath.Join(dir, ".venv")) {
		return EnvInfo{Kind: EnvUV, Dir: dir}
	}

	return EnvInfo{Kind: EnvNone}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ---------------------------------------------------------------------------
// Argument builders (testable without syscall.Exec)
// ---------------------------------------------------------------------------

// BuildUVArgs returns the uv arguments for running a tool in the given environment.
// For pixi environments it returns nil (pixi uses its own python binary).
func BuildUVArgs(tool Tool, env EnvInfo, extraPkgs []string) []string {
	switch env.Kind {
	case EnvPixi:
		return nil

	case EnvUV:
		args := []string{"run", "--project", env.Dir, "--with", tool.Pkg}
		for _, p := range extraPkgs {
			args = append(args, "--with", p)
		}
		return append(args, append([]string{"--"}, tool.UVRun...)...)

	default: // EnvNone — ephemeral
		allPkgs := append([]string{tool.Pkg}, tool.DefaultPkgs...)
		allPkgs = append(allPkgs, extraPkgs...)
		args := []string{"run"}
		for _, p := range allPkgs {
			args = append(args, "--with", p)
		}
		return append(args, append([]string{"--"}, tool.UVRun...)...)
	}
}

// BuildPixiCmd returns the argv for running a tool via a pixi environment's python.
func BuildPixiCmd(dir string, tool Tool) []string {
	python := filepath.Join(dir, ".pixi", "envs", "default", "bin", "python")
	cmd := []string{python, "-m", tool.PixiModule}
	return append(cmd, tool.PixiArgs...)
}

// ---------------------------------------------------------------------------
// Runners (replace process via syscall.Exec)
// ---------------------------------------------------------------------------

// RunTool detects the environment in dir and launches the specified tool.
// If ignoreExisting is true, detection is skipped and an ephemeral session
// is used. It replaces the current process (syscall.Exec).
func RunTool(dir string, tool Tool, extraPkgs []string, ignoreExisting bool) error {
	env := EnvInfo{Kind: EnvNone}
	if !ignoreExisting {
		env = DetectEnv(dir)
	}

	if env.Kind == EnvPixi {
		return runPixiTool(env.Dir, tool)
	}

	uvBin, err := requireUV()
	if err != nil {
		return err
	}

	args := BuildUVArgs(tool, env, extraPkgs)

	switch env.Kind {
	case EnvUV:
		fmt.Printf("Detected uv project in %s\n\n", ui.TUI.Accent().Render(env.Dir))
	default:
		allPkgs := append([]string{tool.Pkg}, tool.DefaultPkgs...)
		allPkgs = append(allPkgs, extraPkgs...)
		fmt.Printf("Starting %s with: %s\n\n", tool.Label, ui.TUI.Accent().Render(strings.Join(allPkgs, ", ")))
	}

	return syscall.Exec(uvBin, append([]string{"uv"}, args...), os.Environ())
}

func runPixiTool(dir string, tool Tool) error {
	cmd := BuildPixiCmd(dir, tool)
	python := cmd[0]

	if _, err := os.Stat(python); os.IsNotExist(err) {
		return fmt.Errorf(
			"pixi environment found but no python at %s\nRun %s first",
			python,
			ui.TUI.Accent().Render("pixi install"),
		)
	}

	fmt.Printf("Detected pixi environment in %s\n\n", ui.TUI.Accent().Render(dir))
	return syscall.Exec(python, cmd, os.Environ())
}

func requireUV() (string, error) {
	bin, err := exec.LookPath("uv")
	if err != nil {
		return "", fmt.Errorf("uv not found — run %s to install it", ui.TUI.Accent().Render("sci doctor check"))
	}
	return bin, nil
}
