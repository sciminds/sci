// Package lab provides access to university lab storage over SSH/SFTP.
//
// The server and paths are fixed for the lab; only the SSH username varies
// per user. Configuration is stored at $XDG_CONFIG_HOME/sci/lab.json.
package lab

import (
	"fmt"
	"path"
	"regexp"

	"github.com/sciminds/cli/internal/sciconfig"
)

// Lab server connection defaults.
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

// configFile is the typed handle to ~/.config/sci/lab.json. Path/load/save
// mechanics (the XDG fallback dance, JSON marshal, 0600 perms) live in
// sciconfig so every sci domain stores config the same way.
var configFile = sciconfig.File[Config]{Name: "lab.json"}

// ConfigPath returns the lab config file path (typically
// $XDG_CONFIG_HOME/sci/lab.json or ~/.config/sci/lab.json).
func ConfigPath() string { return configFile.Path() }

// LoadConfig reads the lab config from disk.
// Returns (nil, nil) if the file does not exist.
func LoadConfig() (*Config, error) { return configFile.Load() }

// SaveConfig writes the lab config to disk with restricted permissions.
func SaveConfig(cfg *Config) error { return configFile.Save(cfg) }

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
