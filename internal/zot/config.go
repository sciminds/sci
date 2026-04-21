// Package zot provides Zotero library management: fast reads from the local
// zotero.sqlite file (opened in immutable mode to avoid WAL contention with
// the running desktop app) and writes via the Zotero Web API. Zotero desktop
// handles sync back to local, so write callers do not need to wait.
//
// The command tree is defined in [github.com/sciminds/cli/internal/zot/cli]
// and is reused by both the standalone `zot` binary (cmd/zot) and the
// integrated `sci zot` subcommand (cmd/sci/zot.go).
package zot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// Config holds Zotero credentials and library location.
//
// OpenAlex fields are optional: populate OpenAlexEmail to opt into the
// polite pool (~10 req/s), OpenAlexAPIKey for the premium tier (~100
// req/s). Both empty = anonymous tier (~1 req/s).
type Config struct {
	APIKey         string `json:"api_key"`                    // Zotero Web API key
	LibraryID      string `json:"library_id"`                 // numeric user ID (library owner)
	DataDir        string `json:"data_dir"`                   // directory containing zotero.sqlite
	OpenAlexEmail  string `json:"openalex_email,omitempty"`   // mailto for polite pool
	OpenAlexAPIKey string `json:"openalex_api_key,omitempty"` // premium tier key
}

// ConfigPath returns the config file path under the XDG config home
// (typically $XDG_CONFIG_HOME/sci/zot.json or ~/.config/sci/zot.json).
func ConfigPath() string {
	return filepath.Join(xdg.ConfigHome, "sci", "zot.json")
}

// LoadConfig reads the zot config from disk.
// Returns (nil, nil) if the file does not exist.
func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse zot config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the zot config to disk with restricted permissions (0600).
func SaveConfig(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RequireConfig loads config and errors if zot is not set up.
func RequireConfig() (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.APIKey == "" || cfg.LibraryID == "" {
		return nil, fmt.Errorf("zot not configured — run 'zot setup' or 'sci zot setup' first")
	}
	return cfg, nil
}

// ConfigExists reports whether a saved zot config file is present on disk.
func ConfigExists() bool {
	_, err := os.Stat(ConfigPath())
	return err == nil
}

// ClearConfig removes the config file if it exists.
func ClearConfig() error {
	if err := os.Remove(ConfigPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// DefaultDataDir probes common Zotero data directory locations in order and
// returns the first that contains a zotero.sqlite file. Returns "" if none
// found — callers should prompt the user.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Zotero"),
		filepath.Join(home, "Desktop", "zotero"),
		filepath.Join(home, "Desktop", "Zotero"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "zotero.sqlite")); err == nil {
			return dir
		}
	}
	return ""
}

// ValidateDataDir checks that the path contains a zotero.sqlite file.
func ValidateDataDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("data directory is required")
	}
	sqlitePath := filepath.Join(dir, "zotero.sqlite")
	if _, err := os.Stat(sqlitePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no zotero.sqlite found in %s", dir)
		}
		return fmt.Errorf("stat %s: %w", sqlitePath, err)
	}
	return nil
}

// ValidateLibraryID checks that the library ID is a non-empty numeric string.
func ValidateLibraryID(id string) error {
	if id == "" {
		return fmt.Errorf("library ID is required")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("library ID must be numeric, got %q", id)
		}
	}
	return nil
}

// ValidateAPIKey checks that the API key is non-empty. The Zotero API does not
// document a fixed format so we only check presence; real validation happens
// on first API call.
func ValidateAPIKey(key string) error {
	if key == "" {
		return fmt.Errorf("API key is required")
	}
	return nil
}
