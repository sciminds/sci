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

// Project holds the detected configuration for a Python project directory.
type Project struct {
	Dir        string
	PkgManager PkgManager
	DocSystem  DocSystem
}

// Detect inspects dir for project markers and returns a Project, or nil if
// no Python project is detected.
func Detect(dir string) *Project {
	pm := detectPkgManager(dir)
	if pm == "" {
		return nil
	}
	return &Project{
		Dir:        dir,
		PkgManager: pm,
		DocSystem:  detectDocSystem(dir),
	}
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
