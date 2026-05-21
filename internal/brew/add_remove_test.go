package brew

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAdd_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "existing"`)
	m := &mockRunner{
		// After DirectInstall, Sync sees both existing and htop as leaves.
		leavesResult:       []string{"existing", "htop"},
		listFormulaeResult: []string{"existing", "htop"},
	}

	result, err := Add(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.directInstallCalls) != 1 {
		t.Fatalf("expected 1 DirectInstall call, got %d", len(m.directInstallCalls))
	}
	if m.directInstallCalls[0].pkg != "htop" {
		t.Errorf("DirectInstall pkg = %q, want %q", m.directInstallCalls[0].pkg, "htop")
	}

	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}

	// Brewfile should now contain htop via Sync.
	got, _ := os.ReadFile(bf)
	if !strings.Contains(string(got), "htop") {
		t.Errorf("Brewfile should contain htop after Sync:\n%s", got)
	}
}

func TestAdd_WithType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "")
	m := &mockRunner{
		listCasksResult: []string{"firefox"},
	}

	_, err := Add(m, bf, "firefox", "cask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.directInstallCalls[0].pkgType != "cask" {
		t.Errorf("DirectInstall pkgType = %q, want %q", m.directInstallCalls[0].pkgType, "cask")
	}
}

func TestAdd_BrewfileUnchangedOnInstallFailure(t *testing.T) {
	t.Parallel()
	original := `brew "existing"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{directInstallErr: errors.New("install failed")}

	_, err := Add(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Brewfile should be untouched — Sync never ran because DirectInstall failed.
	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile should be unchanged.\ngot:  %q\nwant: %q", string(got), original)
	}
}

func TestRemove_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, `brew "htop"`+"\n"+`brew "curl"`+"\n")
	m := &mockRunner{
		// After DirectUninstall, only curl remains as a leaf.
		leavesResult:       []string{"curl"},
		listFormulaeResult: []string{"curl"},
	}

	result, err := Remove(m, bf, "htop", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.directUninstallCalls) != 1 {
		t.Fatalf("expected 1 DirectUninstall call, got %d", len(m.directUninstallCalls))
	}
	if m.directUninstallCalls[0].pkg != "htop" {
		t.Errorf("DirectUninstall pkg = %q, want %q", m.directUninstallCalls[0].pkg, "htop")
	}
	if result.Package != "htop" {
		t.Errorf("result.Package = %q, want %q", result.Package, "htop")
	}

	// htop should be removed from Brewfile via Sync.
	got, _ := os.ReadFile(bf)
	if strings.Contains(string(got), "htop") {
		t.Errorf("Brewfile should not contain htop after Remove:\n%s", got)
	}
}

func TestRemove_BrewfileUnchangedOnUninstallFailure(t *testing.T) {
	t.Parallel()
	original := `brew "htop"` + "\n" + `brew "curl"` + "\n"
	bf := brewfile(t, original)
	m := &mockRunner{directUninstallErr: errors.New("uninstall failed")}

	_, err := Remove(m, bf, "htop", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Brewfile should be untouched — Sync never ran because DirectUninstall failed.
	got, readErr := os.ReadFile(bf)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("Brewfile should be unchanged.\ngot:  %q\nwant: %q", string(got), original)
	}
}
