package proj

// exec.go — project operations that replace the current process via
// [syscall.Exec]. Each function detects the project, builds an argv,
// and execs the appropriate tool. Corresponding Build*Args helpers
// are exported so callers can test argument construction without
// actually executing anything.

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"syscall"
)

// ---------------------------------------------------------------------------
// Dependency management
// ---------------------------------------------------------------------------

// Add installs packages into the project at dir using the detected package
// manager. It replaces the current process (syscall.Exec).
func Add(dir string, pkgs []string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	switch proj.PkgManager {
	case Pixi:
		return execCmd("pixi", slices.Concat([]string{"add"}, pkgs))
	case UV:
		return execCmd("uv", slices.Concat([]string{"add"}, pkgs))
	default:
		return fmt.Errorf("unknown package manager: %s", proj.PkgManager)
	}
}

// Remove uninstalls packages from the project at dir using the detected
// package manager. It replaces the current process (syscall.Exec).
func Remove(dir string, pkgs []string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	switch proj.PkgManager {
	case Pixi:
		return execCmd("pixi", slices.Concat([]string{"remove"}, pkgs))
	case UV:
		return execCmd("uv", slices.Concat([]string{"remove"}, pkgs))
	default:
		return fmt.Errorf("unknown package manager: %s", proj.PkgManager)
	}
}

// ---------------------------------------------------------------------------
// Task runner
// ---------------------------------------------------------------------------

// RunTask runs a project task via the detected package manager.
// It replaces the current process (syscall.Exec).
func RunTask(dir, task string, args []string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	switch proj.PkgManager {
	case Pixi:
		return execCmd("pixi", slices.Concat([]string{"run", task}, args))
	case UV:
		return execCmd("uv", slices.Concat([]string{"run", "poe", task}, args))
	default:
		return fmt.Errorf("unknown package manager: %s", proj.PkgManager)
	}
}

// ---------------------------------------------------------------------------
// Document rendering
// ---------------------------------------------------------------------------

// Render runs the document renderer for the detected doc system.
// It replaces the current process (syscall.Exec).
func Render(dir, target string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	switch proj.DocSystem {
	case Quarto:
		args := []string{"render"}
		if target != "" {
			args = append(args, target)
		}
		return execCmd("quarto", args)
	case Myst:
		return execCmd("npx", []string{"mystmd", "build", "--html"})
	case NoDoc:
		return fmt.Errorf("no doc system detected (no _quarto.yml or myst.yml found)")
	default:
		return fmt.Errorf("unknown doc system: %s", proj.DocSystem)
	}
}

// Preview starts a live preview server for the detected doc system.
// It replaces the current process (syscall.Exec).
func Preview(dir string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	switch proj.DocSystem {
	case Quarto:
		return execCmd("quarto", []string{"preview"})
	case Myst:
		return execCmd("npx", []string{"mystmd", "start"})
	case NoDoc:
		return fmt.Errorf("no doc system detected (no _quarto.yml or myst.yml found)")
	default:
		return fmt.Errorf("unknown doc system: %s", proj.DocSystem)
	}
}

// ---------------------------------------------------------------------------
// Build*Args — argument constructors for testing without syscall.Exec
// ---------------------------------------------------------------------------

// BuildRunTaskArgs constructs the argv for a task runner.
func BuildRunTaskArgs(pm PkgManager, task string, args []string) []string {
	switch pm {
	case Pixi:
		return slices.Concat([]string{"pixi", "run", task}, args)
	case UV:
		return slices.Concat([]string{"uv", "run", "poe", task}, args)
	default:
		return nil
	}
}

// BuildRenderArgs constructs the argv for a render command.
func BuildRenderArgs(ds DocSystem, target string) []string {
	switch ds {
	case Quarto:
		args := []string{"quarto", "render"}
		if target != "" {
			args = append(args, target)
		}
		return args
	case Myst:
		return []string{"npx", "mystmd", "build", "--html"}
	default:
		return nil
	}
}

// BuildPreviewArgs constructs the argv for a preview command.
func BuildPreviewArgs(ds DocSystem) []string {
	switch ds {
	case Quarto:
		return []string{"quarto", "preview"}
	case Myst:
		return []string{"npx", "mystmd", "start"}
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// errNoProject returns a descriptive error when no Python project is found.
func errNoProject(dir string) error {
	return fmt.Errorf("no Python project detected in %s (expected pixi.toml, pyproject.toml with [tool.pixi] or [tool.poe], or uv.lock)", dir)
}

// execCmd replaces the current process with the given command via syscall.Exec.
func execCmd(name string, args []string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return syscall.Exec(bin, slices.Concat([]string{name}, args), os.Environ())
}
