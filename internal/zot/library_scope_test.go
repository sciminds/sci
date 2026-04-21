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
		tc := tc
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
		APIKey:          "k",
		UserID:          "17450224",
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
	if ref.APIPath != "users/17450224" {
		t.Errorf("APIPath = %q, want users/17450224", ref.APIPath)
	}

	ref, err = cfg.Resolve(LibShared)
	if err != nil {
		t.Fatalf("Resolve(shared): %v", err)
	}
	if ref.Scope != LibShared {
		t.Errorf("Scope = %q, want shared", ref.Scope)
	}
	if ref.APIPath != "groups/6506098" {
		t.Errorf("APIPath = %q, want groups/6506098", ref.APIPath)
	}
	if ref.Name != "sciminds" {
		t.Errorf("Name = %q, want sciminds", ref.Name)
	}
}

func TestResolveLibrary_UnknownScope(t *testing.T) {
	t.Parallel()
	cfg := &Config{APIKey: "k", UserID: "1"}
	_, err := cfg.Resolve(LibraryScope("bogus"))
	if err == nil {
		t.Fatal("expected error for unknown scope")
	}
}

func TestResolveLibrary_PersonalWithoutUserID(t *testing.T) {
	t.Parallel()
	cfg := &Config{APIKey: "k"} // no UserID
	_, err := cfg.Resolve(LibPersonal)
	if err == nil {
		t.Fatal("expected error when UserID is unset")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "setup") {
		t.Errorf("err=%v, want mention of setup command", err)
	}
}

func TestResolveLibrary_SharedWithoutGroupID_TriggersProbe(t *testing.T) {
	// When SharedGroupID is empty, Resolve(LibShared) should invoke a
	// configured probe func (lazy auto-detect) rather than failing
	// outright. The probe returns the current account's groups from the
	// Zotero Web API; Resolve picks the single group (or errors with a
	// multi-group message), writes the result back to the Config, and
	// returns the ref.
	withXDGConfigHome(t)

	cfg := &Config{
		APIKey:  "k",
		UserID:  "17450224",
		DataDir: "/tmp/z",
	}
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
	if ref.APIPath != "groups/6506098" {
		t.Errorf("APIPath = %q", ref.APIPath)
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
	cfg := &Config{APIKey: "k", UserID: "17450224"}
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
	cfg := &Config{APIKey: "k", UserID: "17450224"}
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

func TestSaveConfig_EmitsNewFieldNames(t *testing.T) {
	// On save, the JSON must use the new field names (user_id,
	// shared_group_id, shared_group_name) and must not emit the legacy
	// library_id.
	withXDGConfigHome(t)

	cfg := &Config{
		APIKey:          "k",
		UserID:          "1",
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
	for _, want := range []string{"user_id", "shared_group_id", "shared_group_name"} {
		if _, ok := m[want]; !ok {
			t.Errorf("missing field %q in serialized config: %s", want, raw)
		}
	}
	if _, ok := m["library_id"]; ok {
		t.Errorf("legacy library_id still emitted on save: %s", raw)
	}
}
