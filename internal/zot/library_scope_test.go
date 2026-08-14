package zot

// Tests for LibraryScope, LibraryRef, Resolve, ResolveWithProbe, and
// the JSON field names emitted by Config.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestValidateLibraryScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"personal", false},
		{"shared", false},
		{"", true},
		{"user", true},
		{"group", true},
		{"sciminds", true},
		{"Personal", true}, // case-sensitive; flag parser normalizes upstream
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			err := ValidateLibraryScope(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateLibraryScope(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestLibraryScopeConstants(t *testing.T) {
	t.Parallel()
	if string(LibPersonal) != "personal" {
		t.Errorf("LibPersonal = %q, want personal", string(LibPersonal))
	}
	if string(LibShared) != "shared" {
		t.Errorf("LibShared = %q, want shared", string(LibShared))
	}
}

func TestResolveLibrary_PersonalAndShared(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		SharedGroupID:   "6506098",
		SharedGroupName: "sciminds",
		DataDir:         "/tmp/z",
	}

	ref, err := cfg.Resolve(LibPersonal)
	if err != nil {
		t.Fatalf("Resolve(personal): %v", err)
	}
	if ref.Scope != LibPersonal {
		t.Errorf("Scope = %q, want personal", ref.Scope)
	}

	ref, err = cfg.Resolve(LibShared)
	if err != nil {
		t.Fatalf("Resolve(shared): %v", err)
	}
	if ref.Scope != LibShared {
		t.Errorf("Scope = %q, want shared", ref.Scope)
	}
	if ref.Name != "sciminds" {
		t.Errorf("Name = %q, want sciminds", ref.Name)
	}
}

func TestResolveLibrary_UnknownScope(t *testing.T) {
	t.Parallel()
	cfg := &Config{DataDir: "/tmp/z"}
	_, err := cfg.Resolve(LibraryScope("bogus"))
	if err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

// TestResolveLibrary_PersonalNeverFails: the personal library is
// `libraries.type='user'` in the local database, and every Zotero
// installation has exactly one. Nothing about naming it depends on config,
// which is why the old "personal library not configured" branch is gone with
// the user ID it checked.
func TestResolveLibrary_PersonalNeverFails(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	ref, err := cfg.Resolve(LibPersonal)
	if err != nil {
		t.Fatalf("Resolve(personal) on an empty config: %v", err)
	}
	if ref.Scope != LibPersonal {
		t.Errorf("Scope = %q, want personal", ref.Scope)
	}
}

func TestResolveLibrary_SharedWithoutGroupID_TriggersProbe(t *testing.T) {
	// When SharedGroupID is empty, Resolve(LibShared) should invoke a
	// configured probe func (lazy auto-detect) rather than failing
	// outright. The probe lists the local database's group libraries;
	// Resolve picks the single group (or errors with a multi-group
	// message), writes the result back to the Config, and returns the ref.
	withXDGConfigHome(t)

	cfg := &Config{DataDir: "/tmp/z"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	probed := 0
	probe := func() ([]GroupRef, error) {
		probed++
		return []GroupRef{{ID: "6506098", Name: "sciminds"}}, nil
	}

	ref, err := cfg.ResolveWithProbe(LibShared, probe)
	if err != nil {
		t.Fatalf("ResolveWithProbe: %v", err)
	}
	if probed != 1 {
		t.Errorf("probe called %d times, want 1", probed)
	}
	if ref.Name != "sciminds" {
		t.Errorf("Name = %q, want sciminds", ref.Name)
	}
	if cfg.SharedGroupID != "6506098" || cfg.SharedGroupName != "sciminds" {
		t.Errorf("probe did not update cfg: %+v", cfg)
	}

	// Persisted to disk — second load should have the shared fields populated.
	loaded, err := LoadConfig()
	if err != nil || loaded == nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.SharedGroupID != "6506098" {
		t.Errorf("disk cfg SharedGroupID = %q", loaded.SharedGroupID)
	}
}

func TestResolveLibrary_SharedAccountHasNoGroups(t *testing.T) {
	t.Parallel()
	cfg := &Config{DataDir: "/tmp/z"}
	probe := func() ([]GroupRef, error) { return nil, nil }

	_, err := cfg.ResolveWithProbe(LibShared, probe)
	if err == nil {
		t.Fatal("expected error for account with no groups")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "group") {
		t.Errorf("err=%v, want mention of 'group'", err)
	}
}

func TestResolveLibrary_SharedAccountHasMultipleGroups(t *testing.T) {
	// With multiple groups the probe cannot pick one automatically;
	// Resolve returns an error naming the options.
	t.Parallel()
	cfg := &Config{DataDir: "/tmp/z"}
	probe := func() ([]GroupRef, error) {
		return []GroupRef{
			{ID: "1", Name: "alpha"},
			{ID: "2", Name: "beta"},
		}, nil
	}
	_, err := cfg.ResolveWithProbe(LibShared, probe)
	if err == nil {
		t.Fatal("expected error for ambiguous group selection")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Errorf("err=%v, want to list candidate groups", err)
	}
}

// TestSaveConfig_EmitsOnlyTheLocalFields: a saved config names a data
// directory and (optionally) a shared group, and nothing else. The
// credential fields are not merely unset — they must not appear at all, or
// the migration in LoadConfig would keep rewriting a file it just wrote.
func TestSaveConfig_EmitsOnlyTheLocalFields(t *testing.T) {
	withXDGConfigHome(t)

	cfg := &Config{
		SharedGroupID:   "2",
		SharedGroupName: "g",
		DataDir:         "/tmp/z",
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data_dir", "shared_group_id", "shared_group_name"} {
		if _, ok := m[want]; !ok {
			t.Errorf("missing field %q in serialized config: %s", want, raw)
		}
	}
	for _, gone := range []string{"api_key", "user_id", "library_id"} {
		if _, ok := m[gone]; ok {
			t.Errorf("retired field %q still emitted on save: %s", gone, raw)
		}
	}
}
