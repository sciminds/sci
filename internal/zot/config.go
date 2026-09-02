// Package zot is sci's Zotero library driver. Reads — search,
// bibliographies, hygiene reports, exports — are answered from the user's own
// zotero.sqlite (opened in immutable mode to avoid WAL contention with the
// running desktop app) and need no credential. Writes — collections, item
// creation and edits, attachments, tags — go through the Zotero Web API
// ([github.com/sciminds/sci/internal/zot/api]) with the user's own key, and
// refuse without one.
//
// `sci zot setup` records the data directory (required, enables reads) and
// optionally the API key (enables writes). Extracting paper text and
// querying a metered upstream index are out of scope.
//
// The command tree is defined in [github.com/sciminds/sci/internal/zot/cli]
// and mounted under `sci zot` from cmd/sci/zot.go.
package zot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/sciminds/sci/internal/sciconfig"
)

// Config records where the Zotero library lives and, optionally, the
// credential writes need. Every read opens DataDir/zotero.sqlite read-only
// and answers from it, so the data directory alone is a complete
// configuration; APIKey and UserID are consulted only by [RequireAPIConfig],
// which the write verbs call.
//
// UserID is the numeric Zotero user ID the key belongs to. Setup derives it
// from the key via `GET /keys/current` rather than asking for it, so a
// stored key without an ID is a half-finished setup (see RequireAPIConfig).
//
// SharedGroupID + SharedGroupName identify one group library — read out of
// the local `groups` table by setup or the lazy probe — as the "shared"
// target surfaced by --library shared. Multi-group support is a future
// extension.
type Config struct {
	APIKey          string `json:"api_key,omitempty"`           // Zotero Web API key; empty means reads only
	UserID          string `json:"user_id,omitempty"`           // numeric Zotero user ID the key belongs to
	SharedGroupID   string `json:"shared_group_id,omitempty"`   // numeric Zotero group ID for --library shared
	SharedGroupName string `json:"shared_group_name,omitempty"` // human-readable group name (display only)
	DataDir         string `json:"data_dir"`
}

// HasCredential reports whether the config carries a usable write
// credential: a key and the user ID derived from it.
func (c *Config) HasCredential() bool {
	return c != nil && c.APIKey != "" && c.UserID != ""
}

// configFile is the typed handle to ~/.config/sci/zot.json. Path/exists
// mechanics live in sciconfig, but every read and write here goes through the
// RAW helpers: the file is not sci's alone, so sci reads past the keys it does
// not declare and writes without deleting them (see [LoadConfig] and
// [SaveConfig]).
var configFile = sciconfig.File[Config]{Name: "zot.json"}

// ConfigPath returns the zot config file path (typically
// $XDG_CONFIG_HOME/sci/zot.json or ~/.config/sci/zot.json).
//
// The zot commands go through [LoadConfig] / [SaveConfig] and never need the
// path, so the only callers are the tests that assert where the file lands
// and what sci did or did not write to it — the XDG fallback pin, the 0600
// permission check, and the setup tests that prove a refused prompt leaves no
// file behind. None of those are expressible without an observable path.
func ConfigPath() string { return configFile.Path() }

// LoadConfig reads the zot config from disk.
// Returns (nil, nil) if the file does not exist.
//
// Decoding keeps only the fields [Config] declares and ignores every other
// key, which is what lets an older config stay valid with no migration step: a
// file carrying a data directory plus keys sci does not read is complete, and
// must never surface as "zot not configured".
//
// It deliberately does not rewrite the file to drop those keys. Sci declares
// five fields (see [sciOwnedConfigFields]) and treats the rest of the file as
// none of its business — deleting a key on the strength of "sci does not read
// it" would destroy data sci never owned, triggered by nothing heavier than a
// `sci zot search`. Whatever put a key there decides when it goes.
func LoadConfig() (*Config, error) {
	data, err := configFile.LoadRaw()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse zot config: %w", err)
	}
	return &cfg, nil
}

// sciOwnedConfigFields are the keys sci writes into ~/.config/sci/zot.json.
// Everything else in that file belongs to someone else (see [LoadConfig]) and
// survives every write sci makes.
var sciOwnedConfigFields = []string{"api_key", "user_id", "data_dir", "shared_group_id", "shared_group_name"}

// SaveConfig writes the zot config to disk with restricted permissions (0600),
// MERGING into whatever is already there rather than replacing it.
//
// The merge is the load-bearing part, for the reason spelled out on
// [LoadConfig]: marshalling a Config emits only the five fields sci declares,
// so a plain overwrite would delete every other key in the file — turning
// `sci zot setup` into an uninstall for whatever owns them.
//
// cfg is the whole of sci's own state: an empty APIKey is written as "no
// key", not "leave the stored key alone". [Setup] carries a stored credential
// forward for exactly that reason.
func SaveConfig(cfg *Config) error {
	merged, err := mergeConfigInto(existingConfigObject(), cfg)
	if err != nil {
		return err
	}
	return configFile.SaveRaw(merged)
}

// ClearConfig removes the fields sci owns — the data directory and the
// credential — leaving every other key intact, and deletes the file outright
// once nothing is left in it. Deleting the whole file would take keys sci
// never owned with it, a much larger act than the one the user asked for.
func ClearConfig() error {
	obj := existingConfigObject()
	if obj == nil {
		return configFile.Clear()
	}
	for _, k := range sciOwnedConfigFields {
		delete(obj, k)
	}
	if len(obj) == 0 {
		return configFile.Clear()
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	return configFile.SaveRaw(out)
}

// existingConfigObject decodes the config file as a bare JSON object, or nil
// when there is no readable object there. Callers merge into it; a nil result
// simply means the write starts from scratch.
func existingConfigObject() map[string]json.RawMessage {
	data, err := configFile.LoadRaw()
	if err != nil || data == nil {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj
}

// mergeConfigInto overlays cfg's fields onto obj and serializes the result.
// An empty optional field (no shared group) is REMOVED rather than written as
// "", so re-running setup on a library whose group went away does not leave a
// stale name behind.
func mergeConfigInto(obj map[string]json.RawMessage, cfg *Config) ([]byte, error) {
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	own, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(own, &fields); err != nil {
		return nil, err
	}
	for _, k := range sciOwnedConfigFields {
		if v, ok := fields[k]; ok {
			obj[k] = v
		} else {
			delete(obj, k)
		}
	}
	return json.MarshalIndent(obj, "", "  ")
}

// ErrNotConfigured is the sentinel wrapped by [RequireConfig] when no usable
// zot config exists. The CLI layer matches it with errors.Is to attach the
// setup command as a machine-actionable fix.
var ErrNotConfigured = errors.New("zot not configured")

// RequireConfig loads config and errors if zot is not set up.
//
// "Set up" means exactly one thing: a data directory holding a readable
// zotero.sqlite. There is no credential to check — a pure local search must
// not be gated behind a Zotero account, which is what requiring an API key
// used to do to every command in the tree.
func RequireConfig() (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.DataDir == "" {
		return nil, fmt.Errorf("%w — run 'sci zot setup' first", ErrNotConfigured)
	}
	if err := ValidateDataDir(cfg.DataDir); err != nil {
		return nil, fmt.Errorf("%w — %v; run 'sci zot setup' to point sci at your Zotero data directory", ErrNotConfigured, err)
	}
	return cfg, nil
}

// ErrNoCredential is the sentinel wrapped by [RequireAPIConfig] when the
// config names a library but carries no usable API key. The CLI layer matches
// it with errors.Is to attach `sci zot setup --api-key` as the fix.
var ErrNoCredential = errors.New("no Zotero API key configured")

// RequireAPIConfig is [RequireConfig] plus the credential: what every write
// verb needs before it can reach the Zotero Web API.
//
// The local library is still required — a write resolves collection names and
// dedups DOIs against the local mirror — so a key alone is not a configured
// write surface. A key without the user ID setup derives from it is a
// half-finished setup and reports the same sentinel, because the remedy is
// the same: run setup with the key again.
func RequireAPIConfig() (*Config, error) {
	cfg, err := RequireConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.HasCredential() {
		return nil, fmt.Errorf("%w — run 'sci zot setup --api-key <key>' to enable writes (reads work without one)", ErrNoCredential)
	}
	return cfg, nil
}

// ValidateAPIKey refuses the pastes that obviously went wrong — an empty key
// or one carrying whitespace. Keys are otherwise opaque; whether one is real
// is `GET /keys/current`'s question, which setup asks before saving.
func ValidateAPIKey(key string) error {
	if key == "" {
		return fmt.Errorf("API key is required")
	}
	if strings.ContainsFunc(key, unicode.IsSpace) {
		return fmt.Errorf("API key must not contain whitespace")
	}
	return nil
}

// ValidateUserID checks the numeric Zotero user ID `/keys/current` reports.
func ValidateUserID(id string) error {
	if id == "" {
		return fmt.Errorf("user ID is required")
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return fmt.Errorf("user ID must be numeric, got %q", id)
	}
	return nil
}

// ConfigExists reports whether a saved zot config file is present on disk.
func ConfigExists() bool { return configFile.Exists() }

// DefaultDataDir probes common Zotero data directory locations in order and
// returns the first that contains a zotero.sqlite file. Returns "" if none
// found — callers should prompt the user.
//
// The candidate list covers macOS (~/Zotero, ~/Desktop variants) and Linux
// (~/.zotero/zotero for native, ~/snap/zotero-snap/... for the snap package,
// ~/.var/app/org.zotero.Zotero/... for Flatpak). The list is probed on every
// platform; entries that don't exist are simply skipped.
func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Zotero"),
		filepath.Join(home, "Desktop", "zotero"),
		filepath.Join(home, "Desktop", "Zotero"),
		filepath.Join(home, ".zotero", "zotero"),
		filepath.Join(home, "snap", "zotero-snap", "common", "Zotero"),
		filepath.Join(home, ".var", "app", "org.zotero.Zotero", "data", "Zotero"),
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

// LibraryScope names the logical library a command targets.
type LibraryScope string

const (
	// LibPersonal is the Zotero account holder's personal library.
	LibPersonal LibraryScope = "personal"
	// LibShared is the Zotero group library configured as the shared target.
	LibShared LibraryScope = "shared"
	// LibAll is the merged read pool: personal plus the shared group in
	// one call, per-row provenance in Item.Library. Only commands that
	// opted into multi-library handles accept it (search, bib, export
	// --format ndjson, browse); the rest error with the per-library
	// rewrite.
	LibAll LibraryScope = "all"
)

// ValidLibraryScopes is the canonical list used by flag validation and
// error messages. Keep in sync with the LibraryScope constants above.
// LibAll passes flag validation like the others but is additionally
// gated per command — only search and bib accept a merged scope today.
var ValidLibraryScopes = []LibraryScope{LibPersonal, LibShared, LibAll}

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

// LibraryRef identifies a concrete Zotero library for one command. Resolving
// it to SQLite rows is [github.com/sciminds/sci/internal/local]'s job, via the
// LibrarySelector that localSelectorFor builds from Scope.
type LibraryRef struct {
	Scope LibraryScope
	Name  string // display-only ("Personal" or group name)
}

// GroupRef is a lightweight description of a Zotero group library. Used
// by the setup auto-detect + lazy-probe flows.
type GroupRef struct {
	ID   string // numeric groupID as a string (stays consistent with Config.SharedGroupID)
	Name string
}

// GroupProbeFunc lists the group libraries present in the local Zotero
// database. The setup flow and ResolveWithProbe inject a real implementation
// (a read of the `groups` table); tests use fakes. The data directory is
// captured via closure by the caller — the probe takes no args so call sites
// don't have to re-thread it.
type GroupProbeFunc func() ([]GroupRef, error)

// Resolve returns a LibraryRef for the given scope using only the
// fields already in Config. Shared-scope resolution without a cached
// SharedGroupID is an error here — use ResolveWithProbe for lazy
// auto-detection.
//
// Personal never fails: it is `libraries.type='user'` in the local database,
// and there is exactly one of those per Zotero installation.
func (c *Config) Resolve(scope LibraryScope) (LibraryRef, error) {
	switch scope {
	case LibPersonal:
		return LibraryRef{Scope: LibPersonal, Name: "Personal"}, nil
	case LibShared:
		if c.SharedGroupID == "" {
			return LibraryRef{}, fmt.Errorf("shared library not configured — run 'sci zot setup' to auto-detect")
		}
		name := c.SharedGroupName
		if name == "" {
			name = "shared"
		}
		return LibraryRef{Scope: LibShared, Name: name}, nil
	case LibAll:
		// Both member libraries must resolve — a merged pool that quietly
		// lost one would answer a different question than the one asked.
		if _, err := c.Resolve(LibShared); err != nil {
			return LibraryRef{}, err
		}
		return LibraryRef{
			Scope: LibAll,
			Name:  "All libraries",
		}, nil
	default:
		return LibraryRef{}, fmt.Errorf("unknown library scope %q", scope)
	}
}

// ResolveWithProbe is like Resolve but, when the requested scope is
// LibShared and SharedGroupID is empty, it calls probe once to
// auto-detect the group libraries the local database holds. On exactly one
// group, it writes the result back to disk (so subsequent commands stay
// fast) and returns the ref. Zero-group and multi-group libraries error with
// guidance.
func (c *Config) ResolveWithProbe(scope LibraryScope, probe GroupProbeFunc) (LibraryRef, error) {
	if scope != LibShared || c.SharedGroupID != "" || probe == nil {
		return c.Resolve(scope)
	}

	groups, err := probe()
	if err != nil {
		return LibraryRef{}, fmt.Errorf("read Zotero groups: %w", err)
	}
	switch len(groups) {
	case 0:
		return LibraryRef{}, fmt.Errorf("this Zotero library holds no group libraries — --library shared cannot resolve")
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
		return LibraryRef{}, fmt.Errorf("this Zotero library holds multiple groups (%s) — re-run 'sci zot setup' and pick one with --shared-group-id", strings.Join(names, ", "))
	}
}
