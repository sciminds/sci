package cloud

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const defaultBucket = "sci-public"

// DefaultConfigPath returns the path for the R2 credentials file.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sci", "credentials.json")
}

// LoadConfig reads the R2 credentials from disk.
// Returns nil (not an error) if the file does not exist.
// Old flat-format files are migrated in-memory to the new multi-bucket format.
func LoadConfig() (*Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("credentials file is empty — run 'sci cloud setup' to reconfigure")
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	// Migrate legacy flat format → nested BucketConfig.
	if cfg.Public == nil && cfg.LegacyAccessKey != "" {
		bucket := cfg.LegacyBucketName
		if bucket == "" {
			bucket = defaultBucket
		}
		cfg.Public = &BucketConfig{
			AccessKey:  cfg.LegacyAccessKey,
			SecretKey:  cfg.LegacySecretKey,
			BucketName: bucket,
			PublicURL:  cfg.LegacyPublicURL,
		}
	}

	return &cfg, nil
}

// SaveConfig writes the R2 credentials to disk with restricted permissions.
func SaveConfig(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// Clear legacy fields so we only persist the new format.
	clean := *cfg
	clean.LegacyAccessKey = ""
	clean.LegacySecretKey = ""
	clean.LegacyPublicURL = ""
	clean.LegacyBucketName = ""

	data, err := json.MarshalIndent(&clean, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ClearConfig removes the credentials file.
func ClearConfig() error {
	path := ConfigPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RequireConfig loads config and returns an error if not configured.
func RequireConfig() (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("not configured — run 'sci cloud setup' first")
	}
	if cfg.Public == nil || cfg.Public.AccessKey == "" || cfg.Public.SecretKey == "" || cfg.AccountID == "" {
		return nil, fmt.Errorf("incomplete credentials — run 'sci cloud setup' to reconfigure")
	}
	return cfg, nil
}

// ConfigPath returns the credentials path, honoring SCI_CONFIG_PATH env var.
func ConfigPath() string {
	if p := os.Getenv("SCI_CONFIG_PATH"); p != "" {
		return p
	}
	return DefaultConfigPath()
}
