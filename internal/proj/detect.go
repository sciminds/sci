// Package proj detects and manages Python project environments.
//
// It auto-detects the package manager (uv or pixi) and document system
// (Quarto or MyST) by looking for marker files in the project directory:
//
//   - pixi.toml or [tool.pixi] in pyproject.toml → Pixi
//   - [tool.poe] in pyproject.toml or uv.lock → UV
//   - _quarto.yml → Quarto; myst.yml → MyST
//
// All commands that invoke external tools (add, remove, run, render, preview)
// use [syscall.Exec] to replace the current process, giving the user direct
// control of the underlying tool's output. Corresponding Build*Args functions
// are exported for testing without actual execution.
//
// Key functions:
//
//   - [Detect] inspects a directory and returns a [Project]
//   - [Add] / [Remove] install or uninstall packages via uv or pixi
//   - [RunTask] runs a project-defined task (pixi run / uv run poe)
//   - [Render] / [Preview] build or live-preview documents
package proj

import (
	"os"
	"path/filepath"
	"strings"
)

// Kind identifies the high-level type of project sci manages.
type Kind string

// Supported project kinds. Python projects have a package manager (pixi/uv)
// and may include a doc system. Writing projects are pure MyST → Typst PDF
// manuscripts with no Python environment.
const (
	Python  Kind = "python"
	Writing Kind = "writing"
)

// PkgManager identifies the Python package manager used by a project.
type PkgManager string

// Supported package managers.
const (
	Pixi PkgManager = "pixi"
	UV   PkgManager = "uv"
)

// DocSystem identifies the documentation system used by a project.
type DocSystem string

// Supported documentation systems.
const (
	Quarto DocSystem = "quarto"
	Myst   DocSystem = "myst"
	NoDoc  DocSystem = "none"
)

// Project holds the detected configuration for a project directory.
type Project struct {
	Dir        string
	Kind       Kind
	PkgManager PkgManager // empty when Kind == Writing
	DocSystem  DocSystem
}

// Detect inspects dir for project markers and returns a Project, or nil if
// no recognized project is detected. Python markers (pixi.toml, pyproject.toml
// with [tool.pixi]/[tool.poe], or uv.lock) take precedence; otherwise a
// standalone myst.yml indicates a writing-only project.
func Detect(dir string) *Project {
	if pm := detectPkgManager(dir); pm != "" {
		return &Project{
			Dir:        dir,
			Kind:       Python,
			PkgManager: pm,
			DocSystem:  detectDocSystem(dir),
		}
	}
	if fileExists(filepath.Join(dir, "myst.yml")) {
		return &Project{
			Dir:       dir,
			Kind:      Writing,
			DocSystem: Myst,
		}
	}
	return nil
}

func detectPkgManager(dir string) PkgManager {
	// pixi.toml → pixi
	if fileExists(filepath.Join(dir, "pixi.toml")) {
		return Pixi
	}

	// pyproject.toml: check for [tool.pixi] or [tool.poe] / uv.lock.
	// Only match section headers on non-comment lines.
	pyproject := filepath.Join(dir, "pyproject.toml")
	if data, err := os.ReadFile(pyproject); err == nil {
		if tomlContains(string(data), "[tool.pixi") {
			return Pixi
		}
		if tomlContains(string(data), "[tool.poe.") || tomlContains(string(data), "[tool.poe]") {
			return UV
		}
	}

	// uv.lock → uv
	if fileExists(filepath.Join(dir, "uv.lock")) {
		return UV
	}

	return ""
}

// tomlContains checks whether any non-comment line in text contains substr.
func tomlContains(text, substr string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, substr) {
			return true
		}
	}
	return false
}

func detectDocSystem(dir string) DocSystem {
	if fileExists(filepath.Join(dir, "_quarto.yml")) {
		return Quarto
	}
	if fileExists(filepath.Join(dir, "myst.yml")) {
		return Myst
	}
	return NoDoc
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
