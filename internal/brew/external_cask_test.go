package brew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCaskAppPaths_AppArtifact(t *testing.T) {
	t.Parallel()
	jsonData := `{
		"casks": [{
			"token": "vlc",
			"artifacts": [{"app": ["VLC.app"]}]
		}]
	}`
	got, err := parseCaskAppPaths(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"/Applications/VLC.app"}
	if len(got["vlc"]) != 1 || got["vlc"][0] != want[0] {
		t.Errorf("got %v, want %v", got["vlc"], want)
	}
}

func TestParseCaskAppPaths_PkgArtifactWithUninstallDelete(t *testing.T) {
	t.Parallel()
	// Zoom's cask uses a .pkg artifact, so the only hint of the install path
	// is in uninstall.delete. Doctor needs to read this to detect a vendor-installed
	// Zoom (or "Zoom Workplace") and skip the failing reinstall.
	jsonData := `{
		"casks": [{
			"token": "zoom",
			"artifacts": [
				{"uninstall": [{"delete": ["/Applications/zoom.us.app", "/Library/Logs/foo"]}]},
				{"pkg": ["zoomusInstallerFull.pkg"]}
			]
		}]
	}`
	got, err := parseCaskAppPaths(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got["zoom"]) != 1 || got["zoom"][0] != "/Applications/zoom.us.app" {
		t.Errorf("got %v, want [/Applications/zoom.us.app]", got["zoom"])
	}
}

func TestParseCaskAppPaths_RenamedAppArtifact(t *testing.T) {
	t.Parallel()
	// Some casks rename the .app via a {"target": "..."} override.
	jsonData := `{
		"casks": [{
			"token": "weird",
			"artifacts": [{"app": [{"target": "Renamed.app"}]}]
		}]
	}`
	got, err := parseCaskAppPaths(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got["weird"]) != 1 || got["weird"][0] != "/Applications/Renamed.app" {
		t.Errorf("got %v, want [/Applications/Renamed.app]", got["weird"])
	}
}

func TestResolveExternalCasks_DetectsOnDisk(t *testing.T) {
	t.Parallel()
	// Stage a fake app on disk and have CaskAppPaths point at it.
	tmp := t.TempDir()
	app := filepath.Join(tmp, "Demo.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockRunner{
		caskAppPathsResult: map[string][]string{
			"demo":    {app},
			"missing": {filepath.Join(tmp, "Nope.app")},
		},
	}
	got, err := ResolveExternalCasks(m, []string{"demo", "missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "demo" {
		t.Errorf("got %v, want [demo]", got)
	}
}

func TestCollectSnapshotForBrewfile_PopulatesExternalCasks(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	app := filepath.Join(tmp, "Zoom.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &mockRunner{
		// brew doesn't know about zoom — it's installed manually via .pkg.
		listCasksResult: []string{},
		caskAppPathsResult: map[string][]string{
			"zoom": {app},
		},
	}
	content := `cask "zoom"` + "\n"
	snap, err := CollectSnapshotForBrewfile(m, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.ExternalCasks) != 1 || snap.ExternalCasks[0] != "zoom" {
		t.Errorf("ExternalCasks = %v, want [zoom]", snap.ExternalCasks)
	}
	if !snap.IsInstalled("cask", "zoom") {
		t.Error("IsInstalled(cask, zoom) = false, want true via ExternalCasks")
	}
}

func TestCollectSnapshotForBrewfile_SkipsAlreadyTracked(t *testing.T) {
	t.Parallel()
	// If brew already lists the cask, we shouldn't bother probing — saves a
	// brew info call per sync.
	m := &mockRunner{
		listCasksResult: []string{"vlc"},
	}
	content := `cask "vlc"` + "\n"
	snap, err := CollectSnapshotForBrewfile(m, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snap.ExternalCasks) != 0 {
		t.Errorf("ExternalCasks = %v, want empty (vlc already tracked)", snap.ExternalCasks)
	}
	if len(m.caskAppPathsCalls) != 0 {
		t.Errorf("CaskAppPaths called %d times, want 0 (brew already tracks vlc)", len(m.caskAppPathsCalls))
	}
}

func TestSync_KeepsExternalCask(t *testing.T) {
	t.Parallel()
	// User has Zoom installed via the vendor .pkg. brew list --cask doesn't
	// see it. Sync must NOT strip the Brewfile entry — that was issue #1.
	tmp := t.TempDir()
	app := filepath.Join(tmp, "zoom.us.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	bf := brewfile(t, `cask "zoom"`+"\n")
	m := &mockRunner{
		listCasksResult: []string{}, // brew doesn't know
		caskAppPathsResult: map[string][]string{
			"zoom": {app},
		},
	}

	result, err := Sync(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Removed != 0 {
		t.Errorf("expected 0 removed, got %d (external cask was stripped!)", result.Removed)
	}
	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), `cask "zoom"`) {
		t.Errorf("Brewfile should still contain zoom:\n%s", got)
	}
}

func TestInstall_SkipsExternalCask(t *testing.T) {
	t.Parallel()
	// Mirror of TestSync_KeepsExternalCask but for Install: don't try to
	// reinstall a cask whose app is already on disk via a vendor .pkg.
	tmp := t.TempDir()
	app := filepath.Join(tmp, "zoom.us.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	bf := brewfile(t, `cask "zoom"`+"\n")
	m := &mockRunner{
		listCasksResult: []string{},
		caskAppPathsResult: map[string][]string{
			"zoom": {app},
		},
	}

	_, err := Install(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.installCasksCalls) != 0 {
		t.Errorf("expected no InstallCasks calls (external), got %d", len(m.installCasksCalls))
	}
}
