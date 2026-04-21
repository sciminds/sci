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
	"strings"

	"github.com/adrg/xdg"
)

// Config holds Zotero credentials and library targets.
//
// A single API key backs both the user's personal library and any group
// libraries they belong to. UserID identifies the personal library.
// SharedGroupID + SharedGroupName identify one accessible group library
// chosen (via setup or lazy probe) as the "shared" target surfaced by
// --library shared. Multi-group support is a future extension.
type Config struct {
	APIKey          string `json:"api_key"`
	UserID          string `json:"user_id"`                     // numeric Zotero user ID
	SharedGroupID   string `json:"shared_group_id,omitempty"`   // numeric Zotero group ID for --library shared
	SharedGroupName string `json:"shared_group_name,omitempty"` // human-readable group name (display only)
	DataDir         string `json:"data_dir"`
	OpenAlexEmail   string `json:"openalex_email,omitempty"`
	OpenAlexAPIKey  string `json:"openalex_api_key,omitempty"`
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
	if cfg == nil || cfg.APIKey == "" || cfg.UserID == "" {
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

// ValidateUserID checks that the user ID is a non-empty numeric string.
func ValidateUserID(id string) error {
	if id == "" {
		return fmt.Errorf("user ID is required")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("user ID must be numeric, got %q", id)
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

// LibraryScope names the logical library a command targets.
type LibraryScope string

const (
	// LibPersonal is the Zotero account holder's personal library.
	LibPersonal LibraryScope = "personal"
	// LibShared is the Zotero group library configured as the shared target.
	LibShared LibraryScope = "shared"
)

// ValidLibraryScopes is the canonical list used by flag validation and
// error messages. Keep in sync with the LibraryScope constants above.
var ValidLibraryScopes = []LibraryScope{LibPersonal, LibShared}

// ValidateLibraryScope reports whether s matches one of the known scope
// values. Values are case-sensitive to keep flag parsing predictable.
func ValidateLibraryScope(s string) error {
	for _, v := range ValidLibraryScopes {
		if s == string(v) {
			return nil
		}
	}
	names := make([]string, 0, len(ValidLibraryScopes))
	for _, v := range ValidLibraryScopes {
		names = append(names, string(v))
	}
	return fmt.Errorf("invalid library scope %q (expected one of: %s)", s, strings.Join(names, ", "))
}

// LibraryRef identifies a concrete Zotero library for one command.
// Scope + APIPath together are sufficient to build any Web API URL;
// LocalID selects the matching row in zotero.sqlite.
type LibraryRef struct {
	Scope   LibraryScope
	APIPath string // "users/17450224" or "groups/6506098"
	LocalID int64  // Zotero SQLite libraryID
	Name    string // display-only ("Personal" or group name)
}

// GroupRef is a lightweight description of a Zotero group library. Used
// by the setup auto-detect + lazy-probe flows.
type GroupRef struct {
	ID   string // numeric groupID as a string (stays consistent with Config.SharedGroupID)
	Name string
}

// GroupProbeFunc fetches the list of groups accessible to the current
// Zotero account. The setup flow and ResolveWithProbe inject a real
// implementation (api.Client.ListGroups); tests use fakes. Credentials
// are captured via closure by the caller — the probe takes no args so
// call sites don't have to re-thread them.
type GroupProbeFunc func() ([]GroupRef, error)

// Resolve returns a LibraryRef for the given scope using only the
// fields already in Config. Shared-scope resolution without a cached
// SharedGroupID is an error here — use ResolveWithProbe for lazy
// auto-detection.
func (c *Config) Resolve(scope LibraryScope) (LibraryRef, error) {
	switch scope {
	case LibPersonal:
		if c.UserID == "" {
			return LibraryRef{}, fmt.Errorf("personal library not configured — run 'zot setup' first")
		}
		return LibraryRef{
			Scope:   LibPersonal,
			APIPath: "users/" + c.UserID,
			Name:    "Personal",
		}, nil
	case LibShared:
		if c.SharedGroupID == "" {
			return LibraryRef{}, fmt.Errorf("shared library not configured — run 'zot setup' to auto-detect")
		}
		name := c.SharedGroupName
		if name == "" {
			name = "shared"
		}
		return LibraryRef{
			Scope:   LibShared,
			APIPath: "groups/" + c.SharedGroupID,
			Name:    name,
		}, nil
	default:
		return LibraryRef{}, fmt.Errorf("unknown library scope %q", scope)
	}
}

// ResolveWithProbe is like Resolve but, when the requested scope is
// LibShared and SharedGroupID is empty, it calls probe once to
// auto-detect the account's groups. On exactly one group, it writes
// the result back to disk (so subsequent commands stay fast) and
// returns the ref. Zero-group and multi-group accounts error with
// guidance.
func (c *Config) ResolveWithProbe(scope LibraryScope, probe GroupProbeFunc) (LibraryRef, error) {
	if scope != LibShared || c.SharedGroupID != "" || probe == nil {
		return c.Resolve(scope)
	}

	groups, err := probe()
	if err != nil {
		return LibraryRef{}, fmt.Errorf("probe Zotero groups: %w", err)
	}
	switch len(groups) {
	case 0:
		return LibraryRef{}, fmt.Errorf("zotero account %s has no accessible group libraries — --library shared cannot resolve", c.UserID)
	case 1:
		c.SharedGroupID = groups[0].ID
		c.SharedGroupName = groups[0].Name
		// Best-effort persist; the resolve succeeds even if writing
		// fails (next command will re-probe).
		_ = SaveConfig(c)
		return c.Resolve(scope)
	default:
		names := make([]string, 0, len(groups))
		for _, g := range groups {
			names = append(names, g.Name)
		}
		return LibraryRef{}, fmt.Errorf("zotero account has multiple groups (%s) — re-run 'zot setup' and pick one with --shared-group-id", strings.Join(names, ", "))
	}
}
