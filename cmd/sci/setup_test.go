package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/lab"
	"github.com/sciminds/sci/internal/zot"
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

	// A file naming a directory with no zotero.sqlite reads as incomplete,
	// not configured — that file IS the configuration.
	if err := zot.SaveConfig(&zot.Config{DataDir: filepath.Join(t.TempDir(), "gone")}); err != nil {
		t.Fatal(err)
	}
	if configured, summary := zotSetupStatus(); configured {
		t.Errorf("incomplete zot config should not read as configured (summary=%q)", summary)
	}

	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "zotero.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := zot.SaveConfig(&zot.Config{DataDir: dataDir}); err != nil {
		t.Fatal(err)
	}
	if configured, summary := zotSetupStatus(); !configured || !strings.Contains(summary, "writes: off") {
		t.Errorf("zot pointed at a real zotero.sqlite should read as configured, reads only (summary=%q)", summary)
	}

	// A stored key flips the summary and shows up masked in the drill-in.
	if err := zot.SaveConfig(&zot.Config{DataDir: dataDir, APIKey: "zABCDEFGHIJKLMNOPQRSTUVW", UserID: "42"}); err != nil {
		t.Fatal(err)
	}
	if _, summary := zotSetupStatus(); !strings.Contains(summary, "writes: on") {
		t.Errorf("summary with a credential = %q", summary)
	}
	for _, row := range zotFields() {
		if row.label == "api_key" && (strings.Contains(row.value, "EFGHIJ") || !strings.HasPrefix(row.value, "zABC")) {
			t.Errorf("api_key row = %q, want a masked prefix", row.value)
		}
	}
}

func TestToolItems_CarriesKeyAndMark(t *testing.T) {
	entries := []setupEntry{
		{key: "lab", title: "Lab storage", status: func() (bool, string) { return true, "user x" }},
		{key: "zot", title: "Zotero", status: func() (bool, string) { return false, "not configured" }},
	}
	items := toolItems(entries)

	if len(items) != len(entries) {
		t.Fatalf("got %d items, want %d (one per domain, no synthetic Done)", len(items), len(entries))
	}
	for i, want := range entries {
		mi, ok := items[i].(menuItem)
		if !ok {
			t.Fatalf("items[%d] is %T, want menuItem", i, items[i])
		}
		if mi.key != want.key {
			t.Errorf("items[%d].key = %q, want %q", i, mi.key, want.key)
		}
		wantOK, _ := want.status()
		if mi.ok != wantOK {
			t.Errorf("items[%d].ok = %v, want %v", i, mi.ok, wantOK)
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
		if e.key == "" || e.title == "" || e.status == nil || e.fields == nil || e.run == nil {
			t.Errorf("incomplete setup entry: %+v", e)
		}
	}
}
