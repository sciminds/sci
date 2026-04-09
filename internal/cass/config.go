// Package cass provides Canvas LMS and GitHub Classroom integration.
// It syncs course data (students, assignments, submissions, grades) to a
// local SQLite database and supports matching GitHub users to Canvas students,
// diffing local grade edits, and pushing grades back to Canvas.
package cass

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the name of the per-project config file.
const ConfigFile = "cass.yaml"

// Config is the per-project configuration loaded from cass.yaml.
type Config struct {
	Canvas    CanvasConfig     `yaml:"canvas"`
	Classroom *ClassroomConfig `yaml:"classroom,omitempty"`
}

// CanvasConfig holds Canvas LMS connection info.
type CanvasConfig struct {
	URL string `yaml:"url"`
}

// ClassroomConfig holds optional GitHub Classroom connection info.
type ClassroomConfig struct {
	URL   string `yaml:"url"`
	APIID int    `yaml:"api_id,omitempty"` // Resolved API classroom ID (cached after first pull)
}

// HasClassroom returns true if GitHub Classroom is configured.
func (c *Config) HasClassroom() bool {
	return c.Classroom != nil && c.Classroom.URL != ""
}

// CanvasParts extracts the base URL and course ID from the Canvas URL.
func (c *Config) CanvasParts() (baseURL string, courseID int, err error) {
	return ParseCanvasURL(c.Canvas.URL)
}

// ClassroomURLID extracts the URL-embedded ID from the GitHub Classroom URL.
// NOTE: This is NOT the API classroom ID — use ResolveClassroomID to get the API ID.
func (c *Config) ClassroomURLID() (int, error) {
	if !c.HasClassroom() {
		return 0, fmt.Errorf("no classroom configured")
	}
	return ParseClassroomURL(c.Classroom.URL)
}

// ClassroomAPIID returns the resolved API classroom ID.
// If already cached in config, returns the cached value.
func (c *Config) ClassroomAPIID() (int, error) {
	if c.Classroom != nil && c.Classroom.APIID > 0 {
		return c.Classroom.APIID, nil
	}
	return 0, fmt.Errorf("classroom API ID not resolved — run 'sci cass pull' to resolve")
}

// canvasURLRe matches: https://{host}/courses/{id}[/...][?...]
var canvasURLRe = regexp.MustCompile(`^(https?://[^/]+)/courses/(\d+)(?:[/?#].*)?$`)

// ParseCanvasURL extracts the base URL and course ID from a Canvas course URL.
func ParseCanvasURL(rawURL string) (baseURL string, courseID int, err error) {
	// Strip trailing slash before matching (so /courses/123/ works).
	clean := rawURL
	for len(clean) > 0 && clean[len(clean)-1] == '/' {
		clean = clean[:len(clean)-1]
	}
	// Try cleaned version first, fall back to original.
	m := canvasURLRe.FindStringSubmatch(clean)
	if m == nil {
		m = canvasURLRe.FindStringSubmatch(rawURL)
	}
	if m == nil {
		return "", 0, fmt.Errorf("invalid Canvas URL %q — expected https://<host>/courses/<id>", rawURL)
	}
	id, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, fmt.Errorf("invalid course ID in %q: %w", rawURL, err)
	}
	return m[1], id, nil
}

// classroomURLRe matches: https://classroom.github.com/classrooms/{id}[-slug][?...]
var classroomURLRe = regexp.MustCompile(`^https?://classroom\.github\.com/classrooms/(\d+)(?:-[^/?#]+)?(?:[/?#].*)?$`)

// ParseClassroomURL extracts the classroom ID from a GitHub Classroom URL.
func ParseClassroomURL(rawURL string) (classroomID int, err error) {
	m := classroomURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return 0, fmt.Errorf("invalid GitHub Classroom URL %q — expected https://classroom.github.com/classrooms/<id>", rawURL)
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("invalid classroom ID in %q: %w", rawURL, err)
	}
	return id, nil
}

// LoadConfig reads a cass.yaml file from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if cfg.Canvas.URL == "" {
		return nil, fmt.Errorf("%s: canvas.url is required", filepath.Base(path))
	}
	if cfg.Classroom != nil && cfg.Classroom.URL == "" {
		return nil, fmt.Errorf("%s: classroom.url is required when classroom is configured", filepath.Base(path))
	}
	return &cfg, nil
}

// SaveConfig writes a cass.yaml file to the given path.
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// FindConfig walks up from startDir looking for cass.yaml.
// Returns the absolute path to the first cass.yaml found.
func FindConfig(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ConfigFile)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found (searched from %s to /)", ConfigFile, startDir)
		}
		dir = parent
	}
}

// --- Canvas token management ---
// Stored in ~/.config/sci/credentials.json alongside cloud credentials.

// canvasTokenConfig is the JSON structure for Canvas token storage.
// It lives alongside cloud.Config in credentials.json but uses a separate field.
type canvasTokenConfig struct {
	CanvasToken string `json:"canvas_token,omitempty"`
}

// LoadCanvasToken reads the Canvas API token from a credentials file.
func LoadCanvasToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	var cfg canvasTokenConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}
	return cfg.CanvasToken, nil
}

// SaveCanvasToken writes the Canvas API token to a credentials file.
// It preserves existing fields in the JSON file (e.g. cloud credentials).
func SaveCanvasToken(path string, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	// Read existing file to preserve other fields.
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	existing["canvas_token"] = token
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RequireCanvasToken loads the token and returns an error if not configured.
func RequireCanvasToken(path string) (string, error) {
	token, err := LoadCanvasToken(path)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("canvas API token not configured — run 'sci cass setup' first")
	}
	return token, nil
}
