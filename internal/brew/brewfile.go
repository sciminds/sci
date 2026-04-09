// brewfile.go — Brewfile location, parsing, and reconciliation helpers.

package brew

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BrewfileEntry is a parsed line from a Brewfile.
type BrewfileEntry struct {
	Type string // "brew", "cask", "tap", "uv", etc.
	Name string // package name without quotes/extras (e.g. "git", "symbex")
	Line string // original Brewfile line for writing back
}

// Label returns a display label like "git (brew)" or "symbex (uv)".
func (e BrewfileEntry) Label() string {
	return e.Name + " (" + e.Type + ")"
}

// ParseBrewfileEntries parses a Brewfile into structured entries.
func ParseBrewfileEntries(content string) []BrewfileEntry {
	var entries []BrewfileEntry
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		typ := parts[0]
		// Strip quotes and any trailing extras like [recommended] or , with: [...]
		name := strings.Trim(parts[1], `",`)
		// Strip bracket suffixes like "marimo[recommended]"
		if idx := strings.Index(name, "["); idx != -1 {
			name = name[:idx]
		}
		entries = append(entries, BrewfileEntry{Type: typ, Name: name, Line: trimmed})
	}
	return entries
}

// ParseBrewfileNames extracts package names from a Brewfile in declaration order.
func ParseBrewfileNames(content string) []string {
	var names []string
	for _, e := range ParseBrewfileEntries(content) {
		names = append(names, e.Name)
	}
	return names
}

// LocateBrewfile searches for an existing Brewfile in the locations that
// `brew bundle --global` checks, in priority order. Returns the path of the
// first file found, or "" if none exists.
func LocateBrewfile() string {
	for _, p := range brewfileCandidates() {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// brewfileCandidates returns the candidate Brewfile paths in priority order,
// matching `brew bundle --global` resolution.
func brewfileCandidates() []string {
	var paths []string

	// 1. $HOMEBREW_BUNDLE_FILE_GLOBAL
	if v := os.Getenv("HOMEBREW_BUNDLE_FILE_GLOBAL"); v != "" {
		paths = append(paths, v)
	}

	// 2. $XDG_CONFIG_HOME/homebrew/Brewfile
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "homebrew", "Brewfile"))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return paths
	}

	// Also check ~/.config/homebrew/Brewfile (XDG default) if XDG_CONFIG_HOME
	// wasn't set or pointed elsewhere.
	xdgDefault := filepath.Join(home, ".config", "homebrew", "Brewfile")
	if !containsPath(paths, xdgDefault) {
		paths = append(paths, xdgDefault)
	}

	// 3. ~/.homebrew/Brewfile
	paths = append(paths, filepath.Join(home, ".homebrew", "Brewfile"))

	// 4. ~/.Brewfile
	paths = append(paths, filepath.Join(home, ".Brewfile"))

	return paths
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// ResolveBrewfile returns the path to an existing Brewfile, or creates one at
// the default XDG location if none is found. Returns the path and whether the
// file was newly created.
func ResolveBrewfile() (path string, created bool, err error) {
	if found := LocateBrewfile(); found != "" {
		return found, false, nil
	}

	// Create at the default XDG location.
	path, err = ExpandPath(DefaultBrewfile)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, fmt.Errorf("create Brewfile directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		return "", false, fmt.Errorf("create Brewfile: %w", err)
	}
	return path, true, nil
}

// DumpAndDiff runs `brew bundle dump` to capture the system's installed
// packages, then diffs against the Brewfile at path. Returns the entries
// from the dump that are not already declared in the Brewfile.
func DumpAndDiff(r Runner, path string) ([]BrewfileEntry, error) {
	// Dump current system state to a temp file.
	tmp, err := os.CreateTemp("", "sci-brew-dump-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dump file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := r.BundleDump(tmpPath); err != nil {
		return nil, fmt.Errorf("brew bundle dump: %w", err)
	}

	dumpContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read dump: %w", err)
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

	return diffEntries(string(dumpContent), string(existing)), nil
}

// diffEntries returns entries present in dump but not in existing.
// Matching is by (type, name) pair.
func diffEntries(dump, existing string) []BrewfileEntry {
	existingSet := make(map[string]bool)
	for _, e := range ParseBrewfileEntries(existing) {
		existingSet[e.Type+"\t"+e.Name] = true
	}

	var missing []BrewfileEntry
	for _, e := range ParseBrewfileEntries(dump) {
		if !existingSet[e.Type+"\t"+e.Name] {
			missing = append(missing, e)
		}
	}
	return missing
}

// MissingEntries returns entries from required that are not declared in the
// Brewfile at path. Matching is by (type, name) pair.
func MissingEntries(path, required string) ([]BrewfileEntry, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

	return diffEntries(required, string(existing)), nil
}

// AppendEntries appends the given entries to the Brewfile at path,
// preserving existing content. Returns the names of added entries.
func AppendEntries(path string, entries []BrewfileEntry) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Brewfile: %w", err)
	}

	var b strings.Builder
	b.Write(content)
	// Ensure trailing newline before appending.
	if len(content) > 0 && content[len(content)-1] != '\n' {
		b.WriteByte('\n')
	}

	var names []string
	for _, e := range entries {
		b.WriteString(e.Line + "\n")
		names = append(names, e.Name)
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write Brewfile: %w", err)
	}
	return names, nil
}

// WriteTempBrewfile writes content to a temp file and returns its path.
// The caller is responsible for removing it.
func WriteTempBrewfile(content string) (string, error) {
	f, err := os.CreateTemp("", "sci-brewfile-*")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	_ = f.Close()
	return f.Name(), nil
}
