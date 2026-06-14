package main

import (
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/zot"
)

// withXDGConfigHome points config storage at a fresh temp dir so status checks
// read a known-empty (then known-populated) state.
func withXDGConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
}

func TestLabSetupStatus(t *testing.T) {
	withXDGConfigHome(t)

	if configured, _ := labSetupStatus(); configured {
		t.Error("lab should be unconfigured in a fresh config dir")
	}

	if err := lab.SaveConfig(&lab.Config{User: "e3jolly"}); err != nil {
		t.Fatal(err)
	}
	configured, summary := labSetupStatus()
	if !configured {
		t.Error("lab should be configured after SaveConfig")
	}
	if summary == "" {
		t.Error("configured status should carry a non-empty summary")
	}
}

func TestZotSetupStatus(t *testing.T) {
	withXDGConfigHome(t)

	if configured, _ := zotSetupStatus(); configured {
		t.Error("zot should be unconfigured in a fresh config dir")
	}

	// A file present but missing credentials reads as incomplete, not configured.
	if err := zot.SaveConfig(&zot.Config{DataDir: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if configured, summary := zotSetupStatus(); configured {
		t.Errorf("incomplete zot config should not read as configured (summary=%q)", summary)
	}

	if err := zot.SaveConfig(&zot.Config{APIKey: "k", UserID: "123", DataDir: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if configured, _ := zotSetupStatus(); !configured {
		t.Error("zot with api key + user id should read as configured")
	}
}

func TestMenuItems_CarriesKeyAndMark(t *testing.T) {
	statuses := []domainStatus{
		{Key: "lab", Title: "Lab storage", Configured: true, Summary: "user x"},
		{Key: "zot", Title: "Zotero", Configured: false, Summary: "not configured"},
	}
	items := menuItems(statuses)

	if len(items) != len(statuses) {
		t.Fatalf("got %d items, want %d (one per domain, no synthetic Done)", len(items), len(statuses))
	}
	for i, want := range statuses {
		mi, ok := items[i].(menuItem)
		if !ok {
			t.Fatalf("items[%d] is %T, want menuItem", i, items[i])
		}
		if mi.key != want.Key {
			t.Errorf("items[%d].key = %q, want %q", i, mi.key, want.Key)
		}
		if mi.ok != want.Configured {
			t.Errorf("items[%d].ok = %v, want %v", i, mi.ok, want.Configured)
		}
	}
}

func TestSetupStatusResult_JSONAndHuman(t *testing.T) {
	r := setupStatusResult{Domains: []domainStatus{
		{Key: "lab", Title: "Lab storage", Configured: true, Summary: "user x"},
	}}
	if _, ok := r.JSON().(setupStatusResult); !ok {
		t.Error("JSON() should return setupStatusResult")
	}
	if got := r.Human(); got == "" {
		t.Error("Human() should render a non-empty summary")
	}
}

// TestSetupRegistry_RunnableEntries guards that every registered domain is fully
// wired (id, label, status fn, run fn) — a missing field would panic at menu time.
func TestSetupRegistry_RunnableEntries(t *testing.T) {
	for _, e := range setupRegistry() {
		if e.key == "" || e.title == "" || e.status == nil || e.run == nil {
			t.Errorf("incomplete setup entry: %+v", e)
		}
	}
}
