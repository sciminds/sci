package brew

import (
	"errors"
	"testing"
)

func TestInstall_HappyPath(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\n")
	m := &mockRunner{
		// htop not installed.
		listFormulaeResult: []string{},
	}

	result, err := Install(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.installFormulaeCalls) != 1 {
		t.Fatalf("expected 1 InstallFormulae call, got %d", len(m.installFormulaeCalls))
	}
	if m.installFormulaeCalls[0][0] != "htop" {
		t.Errorf("InstallFormulae arg = %q, want %q", m.installFormulaeCalls[0][0], "htop")
	}
	if len(result.Installed) != 1 || result.Installed[0] != "htop" {
		t.Errorf("Installed = %v, want [htop]", result.Installed)
	}
}

func TestInstall_NothingMissing(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "brew \"htop\"\n")
	m := &mockRunner{
		listFormulaeResult: []string{"htop"},
	}

	result, err := Install(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.installFormulaeCalls) != 0 {
		t.Errorf("expected no InstallFormulae calls, got %d", len(m.installFormulaeCalls))
	}
	if len(result.Installed) != 0 {
		t.Errorf("Installed = %v, want empty", result.Installed)
	}
}

func TestInstall_GroupsByType(t *testing.T) {
	t.Parallel()
	bf := brewfile(t, "tap \"oven-sh/bun\"\nbrew \"git\"\nbrew \"curl\"\ncask \"firefox\"\nuv \"marimo\"\n")
	m := &mockRunner{
		// Nothing installed.
		listFormulaeResult: []string{},
		listCasksResult:    []string{},
		tapsResult:         []string{},
		uvToolListResult:   []string{},
	}

	result, err := Install(m, bf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Taps go through DirectInstall.
	if len(m.directInstallCalls) != 1 {
		t.Fatalf("expected 1 DirectInstall call (tap), got %d", len(m.directInstallCalls))
	}
	if m.directInstallCalls[0].pkg != "oven-sh/bun" || m.directInstallCalls[0].pkgType != "tap" {
		t.Errorf("DirectInstall = %+v, want tap oven-sh/bun", m.directInstallCalls[0])
	}

	// Formulae batched.
	if len(m.installFormulaeCalls) != 1 {
		t.Fatalf("expected 1 InstallFormulae call, got %d", len(m.installFormulaeCalls))
	}
	if len(m.installFormulaeCalls[0]) != 2 {
		t.Errorf("expected 2 formulae, got %d", len(m.installFormulaeCalls[0]))
	}

	// Casks batched.
	if len(m.installCasksCalls) != 1 {
		t.Fatalf("expected 1 InstallCasks call, got %d", len(m.installCasksCalls))
	}

	// UV tools batched.
	if len(m.installUVToolsCalls) != 1 {
		t.Fatalf("expected 1 InstallUVTools call, got %d", len(m.installUVToolsCalls))
	}

	// Installed should list all 5 names.
	if len(result.Installed) != 5 {
		t.Errorf("Installed = %v, want 5 items", result.Installed)
	}
}

// ---------------------------------------------------------------------------
// InstallEntries (shared install chain)
// ---------------------------------------------------------------------------

func TestInstallEntries_Empty(t *testing.T) {
	t.Parallel()
	m := &mockRunner{}

	installed, err := InstallEntries(m, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installed != nil {
		t.Errorf("expected nil, got %v", installed)
	}
}

func TestInstallEntries_TapsFirst(t *testing.T) {
	t.Parallel()
	m := &mockRunner{}
	entries := []BrewfileEntry{
		{Type: "brew", Name: "oven-sh/bun/bun"},
		{Type: "tap", Name: "oven-sh/bun"},
	}

	installed, err := InstallEntries(m, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tap must be installed via DirectInstall before formulae.
	if len(m.directInstallCalls) != 1 || m.directInstallCalls[0].pkg != "oven-sh/bun" {
		t.Errorf("DirectInstall = %+v, want tap oven-sh/bun", m.directInstallCalls)
	}
	if len(m.installFormulaeCalls) != 1 || m.installFormulaeCalls[0][0] != "oven-sh/bun/bun" {
		t.Errorf("InstallFormulae = %v, want [oven-sh/bun/bun]", m.installFormulaeCalls)
	}
	if len(installed) != 2 {
		t.Errorf("installed = %v, want 2 items", installed)
	}
}

func TestInstallEntries_FormulaError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{installFormulaeErr: errors.New("permission denied")}
	entries := []BrewfileEntry{
		{Type: "brew", Name: "git"},
		{Type: "cask", Name: "firefox"},
	}

	_, err := InstallEntries(m, entries)
	if err == nil {
		t.Fatal("expected error")
	}
	// Casks should still have been attempted — one failing phase
	// shouldn't block later phases from installing.
	if len(m.installCasksCalls) != 1 {
		t.Errorf("InstallCasks should still be called after formulae error, got %d calls", len(m.installCasksCalls))
	}
}

func TestInstallEntries_CaskError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{installCasksErr: errors.New("cask failed")}
	entries := []BrewfileEntry{
		{Type: "cask", Name: "firefox"},
		{Type: "uv", Name: "marimo"},
	}

	_, err := InstallEntries(m, entries)
	if err == nil {
		t.Fatal("expected error")
	}
	// uv tools should still install — a stuck cask (e.g. one that conflicts
	// with a pre-existing app on disk) must not block the rest of the batch.
	if len(m.installUVToolsCalls) != 1 {
		t.Errorf("InstallUVTools should still be called after cask error, got %d calls", len(m.installUVToolsCalls))
	}
}

func TestInstallEntries_UVError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{installUVToolsErr: errors.New("uv failed")}
	entries := []BrewfileEntry{{Type: "uv", Name: "marimo"}}

	_, err := InstallEntries(m, entries)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallEntries_TapError(t *testing.T) {
	t.Parallel()
	m := &mockRunner{directInstallErr: errors.New("tap failed")}
	entries := []BrewfileEntry{
		{Type: "tap", Name: "oven-sh/bun"},
		{Type: "brew", Name: "git"},
	}

	_, err := InstallEntries(m, entries)
	if err == nil {
		t.Fatal("expected error")
	}
	// Formulae should still be attempted — tap failures shouldn't poison
	// the rest of the install chain.
	if len(m.installFormulaeCalls) != 1 {
		t.Errorf("InstallFormulae should still be called after tap error, got %d calls", len(m.installFormulaeCalls))
	}
}
