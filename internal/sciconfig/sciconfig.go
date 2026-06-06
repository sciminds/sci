// Package sciconfig centralizes how sci stores per-domain configuration under
// the sci config directory ($XDG_CONFIG_HOME/sci, typically ~/.config/sci).
//
// Each domain (lab, zot, …) declares a [File] for its config struct and gets
// consistent path resolution, JSON load/save with 0600 permissions, and
// existence/clear helpers — replacing the XDG-fallback + marshal + permission
// boilerplate that was copy-pasted across packages.
//
// Domains layer their own logic (validation, schema migration, defaulting) on
// top of these primitives; sciconfig owns only the storage mechanics.
package sciconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// appDir is the per-application subdirectory under the XDG config home that
// holds every sci config file.
const appDir = "sci"

// Dir returns the sci config directory ($XDG_CONFIG_HOME/sci or ~/.config/sci).
//
// An empty XDG_CONFIG_HOME (set to "" in the shell, not just unset) is treated
// as "use $HOME/.config" rather than trusting xdg.ConfigHome's fallback — on
// darwin that fallback is ~/Library/Application Support, which is not where our
// config files live.
func Dir() string {
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", appDir)
	}
	return filepath.Join(xdg.ConfigHome, appDir)
}

// Path returns the full path to a named config file under [Dir] (e.g.
// Path("lab.json") → ~/.config/sci/lab.json).
func Path(name string) string {
	return filepath.Join(Dir(), name)
}

// File is a typed handle to one JSON config file under [Dir]. The type
// parameter T is the domain's config struct; declare one per domain, e.g.
//
//	var configFile = sciconfig.File[Config]{Name: "lab.json"}
type File[T any] struct {
	Name string // file name within the sci config dir, e.g. "lab.json"
}

// Path returns the full on-disk path to this config file.
func (f File[T]) Path() string { return Path(f.Name) }

// Exists reports whether the config file is present on disk.
func (f File[T]) Exists() bool {
	_, err := os.Stat(f.Path())
	return err == nil
}

// LoadRaw returns the file's raw bytes, or (nil, nil) if it does not exist.
//
// Most callers want [File.Load]. LoadRaw exists for domains that migrate legacy
// on-disk schemas: they need the original bytes to detect deprecated field
// names before decoding into the current struct.
func (f File[T]) LoadRaw() ([]byte, error) {
	data, err := os.ReadFile(f.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// Load reads and JSON-decodes the config, returning (nil, nil) if the file does
// not exist.
func (f File[T]) Load() (*T, error) {
	data, err := f.LoadRaw()
	if err != nil || data == nil {
		return nil, err
	}
	var cfg T
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", f.Name, err)
	}
	return &cfg, nil
}

// Save writes the config as indented JSON with restricted permissions (0600),
// creating the sci config directory if needed.
func (f File[T]) Save(cfg *T) error {
	path := f.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Clear removes the config file. A missing file is not an error.
func (f File[T]) Clear() error {
	if err := os.Remove(f.Path()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
