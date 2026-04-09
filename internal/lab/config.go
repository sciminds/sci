// Package lab provides access to university lab storage over SSH/SFTP.
//
// The server and paths are fixed for the lab; only the SSH username varies
// per user. Configuration is stored at ~/.config/sci/lab.json.
package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
)

const (
	Host      = "ssrde.ucsd.edu"
	ReadRoot  = "/labs/sciminds"
	WriteRoot = "/labs/sciminds/sci"
)

// Config holds per-user lab connection settings.
type Config struct {
	User string `json:"user"` // SSH username (e.g. "e3jolly")
}

// WriteDir returns the user's writable directory on the server (POSIX path).
func (c *Config) WriteDir() string {
	return path.Join(WriteRoot, c.User)
}

// validUser matches alphanumeric usernames with optional hyphens/underscores/dots.
var validUser = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateUser checks that a username is safe for use in shell commands and paths.
func ValidateUser(user string) error {
	if user == "" {
		return fmt.Errorf("username is required")
	}
	if !validUser.MatchString(user) {
		return fmt.Errorf("invalid username %q — must be alphanumeric (hyphens, underscores, dots allowed)", user)
	}
	return nil
}

// SSHAlias returns the SSH config Host alias for this user.
func (c *Config) SSHAlias() string {
	return "scilab-" + c.User
}

// ConfigPath returns the config file path, honoring SCI_LAB_CONFIG_PATH.
func ConfigPath() string {
	if p := os.Getenv("SCI_LAB_CONFIG_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sci", "lab.json")
}

// LoadConfig reads the lab config from disk.
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
		return nil, fmt.Errorf("parse lab config: %w", err)
	}
	return &cfg, nil
}

// SaveConfig writes the lab config to disk with restricted permissions.
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

// RequireConfig loads config and returns an error if not configured.
func RequireConfig() (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.User == "" {
		return nil, fmt.Errorf("lab not configured — run 'sci lab setup' first")
	}
	return cfg, nil
}
