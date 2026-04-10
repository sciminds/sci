package doctor

import (
	"os"
	"testing"

	"github.com/sciminds/cli/internal/brew"
)

func TestBoolStatus(t *testing.T) {
	if got := boolStatus(true); got != StatusPass {
		t.Errorf("boolStatus(true) = %q, want %q", got, StatusPass)
	}
	if got := boolStatus(false); got != StatusFail {
		t.Errorf("boolStatus(false) = %q, want %q", got, StatusFail)
	}
}

func TestCheckPreflight_Structure(t *testing.T) {
	sec := checkPreflight()

	if sec.Name != "Pre-flight" {
		t.Errorf("section name = %q, want %q", sec.Name, "Pre-flight")
	}
	if len(sec.Checks) != 3 {
		t.Fatalf("expected 3 checks (Homebrew, Xcode CLT, Shell), got %d", len(sec.Checks))
	}

	wantLabels := []string{"Homebrew", "Xcode CLT", "Shell"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
	}
}

func TestCheckIdentity_Structure(t *testing.T) {
	sec := checkIdentity()

	if sec.Name != "Identity" {
		t.Errorf("section name = %q, want %q", sec.Name, "Identity")
	}

	if len(sec.Checks) < 3 {
		t.Fatalf("expected at least 3 checks, got %d", len(sec.Checks))
	}

	wantLabels := []string{"Git user.name", "Git user.email", "GitHub CLI auth"}
	for i, want := range wantLabels {
		if sec.Checks[i].Label != want {
			t.Errorf("check[%d].Label = %q, want %q", i, sec.Checks[i].Label, want)
		}
	}

	for _, c := range sec.Checks {
		switch c.Status {
		case StatusPass, StatusFail, StatusWarn:
		default:
			t.Errorf("check %q has unknown status %q", c.Label, c.Status)
		}
		if c.Message == "" {
			t.Errorf("check %q has empty message", c.Label)
		}
	}
}

func TestRunAll_ReturnsSections(t *testing.T) {
	sections := RunAll()

	if len(sections) != len(checkFuncs) {
		t.Fatalf("RunAll returned %d sections, want %d", len(sections), len(checkFuncs))
	}

	for i, sec := range sections {
		if sec.Name == "" {
			t.Errorf("section[%d] has empty name", i)
		}
		if len(sec.Checks) == 0 {
			t.Errorf("section %q has no checks", sec.Name)
		}
	}

	if sections[0].Name != "Pre-flight" {
		t.Errorf("first section = %q, want %q", sections[0].Name, "Pre-flight")
	}
	if sections[1].Name != "Identity" {
		t.Errorf("second section = %q, want %q", sections[1].Name, "Identity")
	}
}

func TestParseBrewfileNames(t *testing.T) {
	content := `brew "git"
brew "uv"
# a comment
cask "visual-studio-code"
`
	names := brew.ParseBrewfileNames(content)
	want := []string{"git", "uv", "visual-studio-code"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestParseBrewfileNames_Empty(t *testing.T) {
	names := brew.ParseBrewfileNames("")
	if len(names) != 0 {
		t.Errorf("expected empty, got %v", names)
	}
}

func TestBrewfileEmbedded(t *testing.T) {
	if Brewfile == "" {
		t.Fatal("embedded Brewfile is empty")
	}
	names := brew.ParseBrewfileNames(Brewfile)
	if len(names) == 0 {
		t.Fatal("embedded Brewfile has no packages")
	}
}

func TestRunToolChecks(t *testing.T) {
	// Write a Brewfile containing the embedded required packages so
	// BundleCheck has something to check against.
	tmpFile, err := brew.WriteTempBrewfile(Brewfile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	mock := &mockBrewRunner{missing: []string{"uv"}}
	infos := RunToolChecks(mock, tmpFile)

	if len(infos) == 0 {
		t.Fatal("expected tool infos from embedded Brewfile")
	}

	// uv should be marked as not installed.
	for _, ti := range infos {
		if ti.Name == "uv" && ti.Installed {
			t.Error("expected uv to be marked as not installed")
		}
		if ti.Name == "git" && !ti.Installed {
			t.Error("expected git to be marked as installed")
		}
	}
}

// mockBrewRunner implements brew.Runner for testing.
type mockBrewRunner struct {
	missing      []string
	installCalls []string
	installErr   error
}

func (m *mockBrewRunner) BundleAdd(_, _, _ string) error             { return nil }
func (m *mockBrewRunner) BundleRemove(_, _, _ string) error          { return nil }
func (m *mockBrewRunner) BundleDump(_ string) error                  { return nil }
func (m *mockBrewRunner) BundleDumpLive(_ string, _, _ func()) error { return nil }
func (m *mockBrewRunner) BundleCleanup(_ string, _ func(string), _, _ func()) (string, error) {
	return "", nil
}
func (m *mockBrewRunner) BundleList(_, _ string) ([]string, error) { return nil, nil }
func (m *mockBrewRunner) Info(_ []string, _ bool) ([]brew.PackageInfo, error) {
	return nil, nil
}

func (m *mockBrewRunner) BundleInstall(file string, _ func(string), _, _ func()) (string, error) {
	m.installCalls = append(m.installCalls, file)
	return "installed", m.installErr
}

func (m *mockBrewRunner) BundleCheck(_ string) ([]string, error) {
	return m.missing, nil
}

func (m *mockBrewRunner) Update(_ func(string), _, _ func()) error            { return nil }
func (m *mockBrewRunner) Outdated() ([]brew.OutdatedPackage, error)           { return nil, nil }
func (m *mockBrewRunner) Upgrade(_ func(string), _, _ func()) (string, error) { return "", nil }
func (m *mockBrewRunner) UVOutdated() ([]brew.OutdatedPackage, error) {
	return nil, nil
}
func (m *mockBrewRunner) UVUpgrade(_ func(string)) (string, error) { return "", nil }
func (m *mockBrewRunner) UVToolList() ([]string, error)            { return nil, nil }
