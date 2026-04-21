package zot

// Phase-5 tests for Setup's shared-group auto-detect + explicit-pick paths.

import (
	"strings"
	"testing"
)

func TestSetup_AutoDetectsSingleGroup(t *testing.T) {
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	probe := func(_, userID string) ([]GroupRef, error) {
		if userID != "17450224" {
			t.Errorf("probe got userID=%q", userID)
		}
		return []GroupRef{{ID: "6506098", Name: "sciminds"}}, nil
	}

	_, err := Setup(SetupInput{
		APIKey:     "k",
		UserID:     "17450224",
		DataDir:    dir,
		GroupProbe: probe,
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.UserID != "17450224" {
		t.Errorf("UserID = %q", cfg.UserID)
	}
	if cfg.SharedGroupID != "6506098" {
		t.Errorf("SharedGroupID = %q, want auto-detected 6506098", cfg.SharedGroupID)
	}
	if cfg.SharedGroupName != "sciminds" {
		t.Errorf("SharedGroupName = %q", cfg.SharedGroupName)
	}
}

func TestSetup_ZeroGroupsIsOK(t *testing.T) {
	// An account with no groups is a valid state — setup saves only the
	// personal fields and a later `--library shared` command surfaces
	// the account-state error.
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	probe := func(_, _ string) ([]GroupRef, error) { return nil, nil }

	_, err := Setup(SetupInput{APIKey: "k", UserID: "17450224", DataDir: dir, GroupProbe: probe})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	cfg, _ := LoadConfig()
	if cfg == nil || cfg.UserID != "17450224" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.SharedGroupID != "" {
		t.Errorf("SharedGroupID = %q, want empty for zero-group account", cfg.SharedGroupID)
	}
}

func TestSetup_MultipleGroupsRequiresExplicitChoice(t *testing.T) {
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	probe := func(_, _ string) ([]GroupRef, error) {
		return []GroupRef{
			{ID: "1", Name: "alpha"},
			{ID: "2", Name: "beta"},
		}, nil
	}

	_, err := Setup(SetupInput{APIKey: "k", UserID: "17450224", DataDir: dir, GroupProbe: probe})
	if err == nil {
		t.Fatal("expected error when account has multiple groups and none is pre-selected")
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
		t.Errorf("err=%v, want groups listed", err)
	}

	// Now pre-select one via SharedGroupID — should succeed.
	_, err = Setup(SetupInput{
		APIKey:          "k",
		UserID:          "17450224",
		DataDir:         dir,
		SharedGroupID:   "2",
		SharedGroupName: "beta",
		GroupProbe:      probe,
	})
	if err != nil {
		t.Fatalf("Setup with explicit group: %v", err)
	}
	cfg, _ := LoadConfig()
	if cfg.SharedGroupID != "2" || cfg.SharedGroupName != "beta" {
		t.Errorf("cfg = %+v, want beta/2", cfg)
	}
}

func TestSetup_NilProbeLeavesSharedBlank(t *testing.T) {
	// No probe (offline / missing API key) — setup completes with
	// shared unconfigured; no error.
	withXDGConfigHome(t)
	dir := mkDataDir(t)

	_, err := Setup(SetupInput{APIKey: "k", UserID: "17450224", DataDir: dir})
	if err != nil {
		t.Fatalf("Setup(nil probe): %v", err)
	}
	cfg, _ := LoadConfig()
	if cfg.SharedGroupID != "" {
		t.Errorf("SharedGroupID = %q, want empty when no probe supplied", cfg.SharedGroupID)
	}
}
