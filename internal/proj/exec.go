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
	if proj.Kind == Writing {
		return errWritingNoPkgManager
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
	if proj.Kind == Writing {
		return errWritingNoPkgManager
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
	if proj.Kind == Writing {
		return errWritingNoPkgManager
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
// It replaces the current process (syscall.Exec). Writing projects render
// the Typst PDF (`mystmd build --pdf`); python+myst projects render the
// HTML site (`mystmd build --html`).
func Render(dir, target string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	args := BuildRenderArgs(proj.Kind, proj.DocSystem, target)
	if args == nil {
		if proj.DocSystem == NoDoc {
			return fmt.Errorf("no doc system detected (no _quarto.yml or myst.yml found)")
		}
		return fmt.Errorf("unknown doc system: %s", proj.DocSystem)
	}
	return execCmd(args[0], args[1:])
}

// Preview starts a live preview server for the detected doc system.
// It replaces the current process (syscall.Exec).
func Preview(dir string) error {
	proj := Detect(dir)
	if proj == nil {
		return errNoProject(dir)
	}

	args := BuildPreviewArgs(proj.Kind, proj.DocSystem)
	if args == nil {
		if proj.DocSystem == NoDoc {
			return fmt.Errorf("no doc system detected (no _quarto.yml or myst.yml found)")
		}
		return fmt.Errorf("unknown doc system: %s", proj.DocSystem)
	}
	return execCmd(args[0], args[1:])
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
// Writing+Myst builds the Typst PDF; Python+Myst builds the HTML site.
func BuildRenderArgs(kind Kind, ds DocSystem, target string) []string {
	switch ds {
	case Quarto:
		args := []string{"quarto", "render"}
		if target != "" {
			args = append(args, target)
		}
		return args
	case Myst:
		if kind == Writing {
			return []string{"npx", "mystmd", "build", "--pdf"}
		}
		return []string{"npx", "mystmd", "build", "--html"}
	default:
		return nil
	}
}

// BuildPreviewArgs constructs the argv for a preview command. Writing and
// python+myst projects share `mystmd start` (the live preview is HTML either
// way; PDF output is rebuilt on save inside the preview).
func BuildPreviewArgs(_ Kind, ds DocSystem) []string {
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

// errNoProject returns a descriptive error when no project is found.
func errNoProject(dir string) error {
	return fmt.Errorf("no project detected in %s (expected pixi.toml, pyproject.toml with [tool.pixi] or [tool.poe], uv.lock, or myst.yml)", dir)
}

// errWritingNoPkgManager is returned when package-manager commands run inside
// a writing-only project. Writing projects have no Python environment to
// install into.
var errWritingNoPkgManager = fmt.Errorf("this is a writing project — no package manager to install into. Use `sci proj render` to build the PDF")

// execCmd replaces the current process with the given command via syscall.Exec.
func execCmd(name string, args []string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return syscall.Exec(bin, slices.Concat([]string{name}, args), os.Environ())
}
