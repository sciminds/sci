package doctor

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/brew"
)

// installedSnap returns a mock whose snapshot reports `installed` as
// already-present formulae (so missingSet treats everything else as missing).
func installedSnap(installed ...string) *mockBrewRunner {
	return &mockBrewRunner{
		listFormulaeResult: installed,
		leavesResult:       installed,
	}
}

func TestResolveOptionalSet_Include(t *testing.T) {
	mock := installedSnap() // nothing installed
	got, err := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"bat", "fd"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := entryNames(got)
	if !slices.Equal(names, []string{"bat", "fd"}) {
		t.Errorf("got %v, want [bat fd]", names)
	}
}

func TestResolveOptionalSet_IncludeSkipsAlreadyInstalled(t *testing.T) {
	mock := installedSnap("bat") // bat already there
	got, err := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"bat", "fd"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := entryNames(got)
	if !slices.Equal(names, []string{"fd"}) {
		t.Errorf("expected installed entries filtered out, got %v", names)
	}
}

func TestResolveOptionalSet_ExcludeSkipsCask(t *testing.T) {
	mock := installedSnap()
	got, err := ResolveOptionalSet(mock, OptionalFilter{Exclude: []string{"quarto"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range got {
		if e.Name == "quarto" {
			t.Errorf("--exclude quarto must drop quarto from the resolved set")
		}
	}
	if len(got) == 0 {
		t.Error("--exclude quarto should still yield the rest of BrewfileOptional")
	}
}

func TestResolveOptionalSet_AllReturnsAllMissing(t *testing.T) {
	mock := installedSnap()
	got, err := ResolveOptionalSet(mock, OptionalFilter{All: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := len(brew.ParseBrewfileEntries(BrewfileOptional))
	if len(got) != total {
		t.Errorf("--all with nothing installed should resolve to all %d entries, got %d", total, len(got))
	}
}

func TestResolveOptionalSet_UnknownIncludeErrors(t *testing.T) {
	mock := installedSnap()
	_, err := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"nonexistent-tool"}})
	if err == nil {
		t.Fatal("expected error for unknown tool in --include")
	}
	if !strings.Contains(err.Error(), "nonexistent-tool") {
		t.Errorf("error should name the unknown tool, got: %v", err)
	}
}

func TestResolveOptionalSet_UnknownExcludeErrors(t *testing.T) {
	mock := installedSnap()
	_, err := ResolveOptionalSet(mock, OptionalFilter{Exclude: []string{"nope"}})
	if err == nil {
		t.Fatal("expected error for unknown tool in --exclude")
	}
}

func TestInstallOptionalTools_AllSucceed(t *testing.T) {
	mock := installedSnap()
	entries, _ := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"bat", "fd"}})

	result, err := InstallOptionalTools(mock, entries, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result.Installed, []string{"bat", "fd"}) {
		t.Errorf("Installed = %v, want [bat fd]", result.Installed)
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failures, got %v", result.Failed)
	}
	if len(mock.directInstallCalls) != 2 {
		t.Errorf("expected 2 DirectInstall calls, got %d", len(mock.directInstallCalls))
	}
}

func TestInstallOptionalTools_ContinuesOnFailure(t *testing.T) {
	mock := installedSnap()
	mock.directInstallErrors = map[string]error{
		"bat": errors.New("brew exploded"),
	}
	entries, _ := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"bat", "fd"}})

	result, err := InstallOptionalTools(mock, entries, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(result.Installed, []string{"fd"}) {
		t.Errorf("Installed = %v, want only [fd] (bat failed)", result.Installed)
	}
	if len(result.Failed) != 1 || result.Failed[0].Name != "bat" {
		t.Errorf("Failed = %v, want one entry for bat", result.Failed)
	}
	if !strings.Contains(result.Failed[0].Error, "brew exploded") {
		t.Errorf("failed entry should preserve install error message, got %q", result.Failed[0].Error)
	}
	// Both installs were attempted — continue-on-error semantics.
	if len(mock.directInstallCalls) != 2 {
		t.Errorf("expected 2 DirectInstall attempts, got %d", len(mock.directInstallCalls))
	}
}

func TestInstallOptionalTools_DryRunInstallsNothing(t *testing.T) {
	mock := installedSnap()
	entries, _ := ResolveOptionalSet(mock, OptionalFilter{Include: []string{"bat", "fd"}})

	result, err := InstallOptionalTools(mock, entries, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.DryRun {
		t.Error("DryRun must be true")
	}
	if !slices.Equal(result.Installed, []string{"bat", "fd"}) {
		t.Errorf("dry-run Installed should list intended tools, got %v", result.Installed)
	}
	if len(mock.directInstallCalls) != 0 {
		t.Errorf("dry-run must not call DirectInstall, got %d calls", len(mock.directInstallCalls))
	}
}

func TestInstallOptionalTools_EmptyReturnsEmpty(t *testing.T) {
	mock := installedSnap()
	result, err := InstallOptionalTools(mock, nil, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Installed) != 0 || len(result.Failed) != 0 {
		t.Errorf("empty input must produce empty result, got %+v", result)
	}
}

func TestOptionalSetupResult_HumanDryRun(t *testing.T) {
	r := OptionalSetupResult{Installed: []string{"bat", "fd"}, DryRun: true}
	out := r.Human()
	if !strings.Contains(out, "Dry run") {
		t.Errorf("dry-run human output should say 'Dry run', got: %q", out)
	}
	if !strings.Contains(out, "+ bat") || !strings.Contains(out, "+ fd") {
		t.Errorf("dry-run output should list both tools, got: %q", out)
	}
}

func TestOptionalSetupResult_HumanBulkFailure(t *testing.T) {
	r := OptionalSetupResult{
		Installed: []string{"fd"},
		Failed:    []FailedInstall{{Name: "bat", Error: "boom"}},
	}
	out := r.Human()
	if !strings.Contains(out, "Installed 1") {
		t.Errorf("output should show installed count, got: %q", out)
	}
	if !strings.Contains(out, "Failed 1") || !strings.Contains(out, "bat: boom") {
		t.Errorf("output should detail the failure, got: %q", out)
	}
}

func TestOptionalSetupResult_HumanSingleUnchanged(t *testing.T) {
	r := OptionalSetupResult{Installed: []string{"bat"}}
	out := r.Human()
	if !strings.Contains(out, "Installed 1 tools: bat") {
		t.Errorf("single-install human path should be preserved, got: %q", out)
	}
}

func entryNames(entries []brew.BrewfileEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}
