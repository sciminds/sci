package app

// app_test.go — fspicker-specific tests. The browser primitive's own
// behaviour (navigation, two-press confirm, refresh, Enter-on-leaf
// fall-through) is covered in internal/uikit/browser. Here we cover:
//   - Entry rendering (folder slash, file size/mtime description)
//   - Provider sort / hidden filter / parent clamp / breadcrumb
//   - Action wiring (pick sets State.Picked + quits; toggle flips)

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sciminds/cli/internal/uikit/browser"
)

// ── Entry rendering ─────────────────────────────────────────────────────────

func TestEntry_Title_FolderHasTrailingSlash(t *testing.T) {
	t.Parallel()
	dir := Entry{Name: "data", Dir: true}
	if dir.Title() != "data/" {
		t.Errorf("folder Title = %q, want %q", dir.Title(), "data/")
	}
	file := Entry{Name: "results.csv"}
	if file.Title() != "results.csv" {
		t.Errorf("file Title = %q, want %q", file.Title(), "results.csv")
	}
}

func TestEntry_Description_FolderVsFile(t *testing.T) {
	t.Parallel()
	dir := Entry{Name: "data", Dir: true}
	if !strings.Contains(dir.Description(), "folder") {
		t.Errorf("folder Description = %q, want to contain 'folder'", dir.Description())
	}
	// File with no Info still renders ("?"), no panic.
	file := Entry{Name: "broken.csv"}
	if got := file.Description(); !strings.Contains(got, "?") {
		t.Errorf("missing-Info Description = %q, want to contain '?'", got)
	}
}

func TestEntry_FilterValue_UsesName(t *testing.T) {
	t.Parallel()
	if got := (Entry{Name: "credit.csv"}).FilterValue(); got != "credit.csv" {
		t.Errorf("FilterValue = %q, want credit.csv", got)
	}
}

// ── Provider ────────────────────────────────────────────────────────────────

// seedDir builds a small tree under t.TempDir():
//
//	<root>/
//	  .hidden        (file)
//	  alpha/         (dir)
//	  Beta.csv       (file, 100 bytes)
//	  zeta/          (dir)
func seedDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "alpha"))
	mustMkdir(t, filepath.Join(root, "zeta"))
	mustWrite(t, filepath.Join(root, "Beta.csv"), strings.Repeat("x", 100))
	mustWrite(t, filepath.Join(root, ".hidden"), "secret")
	return root
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestProvider_Children_DirsFirstThenAlpha(t *testing.T) {
	t.Parallel()
	root := seedDir(t)
	p := NewProvider(root, nil, &State{})

	msg, ok := p.Children(root)().(browser.ChildrenMsg)
	if !ok {
		t.Fatalf("Children emitted %T, want ChildrenMsg", msg)
	}
	names := entryNames(msg.Entries)
	// Dirs (alpha, zeta) first — alphabetical, case-insensitive — then files (Beta.csv).
	// .hidden is filtered out by default.
	want := []string{"alpha", "zeta", "Beta.csv"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("entries = %v, want %v", names, want)
	}
}

func TestProvider_Children_HidesDotfilesByDefault(t *testing.T) {
	t.Parallel()
	root := seedDir(t)
	p := NewProvider(root, nil, &State{})

	msg := p.Children(root)().(browser.ChildrenMsg)
	for _, e := range msg.Entries {
		if strings.HasPrefix(e.(Entry).Name, ".") {
			t.Errorf("hidden entry %q surfaced with ShowHidden=false", e.(Entry).Name)
		}
	}
}

func TestProvider_Children_ShowHiddenIncludesDotfiles(t *testing.T) {
	t.Parallel()
	root := seedDir(t)
	state := &State{}
	state.ToggleHidden() // turn on
	p := NewProvider(root, nil, state)

	msg := p.Children(root)().(browser.ChildrenMsg)
	names := entryNames(msg.Entries)
	if !contains(names, ".hidden") {
		t.Errorf("entries = %v, want to include .hidden", names)
	}
}

func TestProvider_Children_FilterApplies(t *testing.T) {
	t.Parallel()
	root := seedDir(t)
	// Only .csv files (and any dir, since filter only runs on entries
	// that survive the hidden check; we let dirs through explicitly).
	csvOnly := func(de os.DirEntry) bool {
		return de.IsDir() || strings.HasSuffix(de.Name(), ".csv")
	}
	p := NewProvider(root, csvOnly, &State{})

	msg := p.Children(root)().(browser.ChildrenMsg)
	names := entryNames(msg.Entries)
	want := []string{"alpha", "zeta", "Beta.csv"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("filtered entries = %v, want %v", names, want)
	}
}

func TestProvider_Children_ErrorOnMissingDir(t *testing.T) {
	t.Parallel()
	p := NewProvider("/nonexistent-path-zzz", nil, &State{})
	msg := p.Children("/nonexistent-path-zzz/also-missing")().(browser.ChildrenMsg)
	if msg.Err == nil {
		t.Error("Children on missing dir returned nil Err; want non-nil")
	}
}

func TestProvider_Parent_ClampsAtFilesystemRoot(t *testing.T) {
	t.Parallel()
	p := NewProvider("/", nil, &State{})
	if runtime.GOOS == "windows" {
		t.Skip("path semantics differ on windows")
	}
	if got := p.Parent("/"); got != "/" {
		t.Errorf("Parent(/) = %q, want /", got)
	}
	if got := p.Parent("/foo"); got != "/" {
		t.Errorf("Parent(/foo) = %q, want /", got)
	}
	if got := p.Parent("/foo/bar"); got != "/foo" {
		t.Errorf("Parent(/foo/bar) = %q, want /foo", got)
	}
}

func TestProvider_Breadcrumb_CollapsesHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir available")
	}
	p := NewProvider(home, nil, &State{})
	if got := p.Breadcrumb(home); got != "~" {
		t.Errorf("Breadcrumb(home) = %q, want ~", got)
	}
	deeper := filepath.Join(home, "Documents", "x")
	if got := p.Breadcrumb(deeper); got != "~"+strings.TrimPrefix(deeper, home) {
		t.Errorf("Breadcrumb(deeper) = %q, want ~/...", got)
	}
	if got := p.Breadcrumb("/tmp"); got != "/tmp" {
		t.Errorf("Breadcrumb(/tmp) = %q, want /tmp (unchanged)", got)
	}
}

// ── State ───────────────────────────────────────────────────────────────────

func TestState_ToggleHidden_Flips(t *testing.T) {
	t.Parallel()
	s := &State{}
	if s.ShowHidden() {
		t.Fatal("ShowHidden default = true, want false")
	}
	if got := s.ToggleHidden(); !got {
		t.Errorf("ToggleHidden first call = false, want true")
	}
	if !s.ShowHidden() {
		t.Error("ShowHidden after first toggle = false, want true")
	}
	if got := s.ToggleHidden(); got {
		t.Errorf("ToggleHidden second call = true, want false")
	}
}

// ── Actions ─────────────────────────────────────────────────────────────────

func TestUploadAction_BoundToU(t *testing.T) {
	t.Parallel()
	actions := BuildActions(&State{})
	findAction(t, actions, "u") // fails the test if absent
}

func TestUploadAction_AppliesToFilesAndDirs(t *testing.T) {
	t.Parallel()
	actions := BuildActions(&State{})
	up := findAction(t, actions, "u")
	if up.AppliesTo != nil {
		t.Error("upload.AppliesTo != nil; want nil (applies to files AND dirs, like browse's download)")
	}
}

func TestUploadAction_Run_SetsPickedAndQuits_NotForce(t *testing.T) {
	t.Parallel()
	state := &State{}
	actions := BuildActions(state)
	up := findAction(t, actions, "u")
	e := Entry{Abs: "/abs/path/myrepo", Name: "myrepo", Dir: true}

	cmd := up.Run(e)
	if state.Picked != "/abs/path/myrepo" {
		t.Errorf("state.Picked = %q, want /abs/path/myrepo", state.Picked)
	}
	if state.Force {
		t.Error("state.Force = true after 'u'; want false")
	}
	if cmd == nil || !isQuitMsg(cmd()) {
		t.Errorf("upload Run = %v, want tea.QuitMsg", cmd)
	}
}

func TestForceUploadAction_BoundToCapitalU(t *testing.T) {
	t.Parallel()
	actions := BuildActions(&State{})
	findAction(t, actions, "U")
}

func TestForceUploadAction_Run_SetsForceAndQuits(t *testing.T) {
	t.Parallel()
	state := &State{}
	actions := BuildActions(state)
	force := findAction(t, actions, "U")
	e := Entry{Abs: "/abs/path/results.csv", Name: "results.csv"}

	cmd := force.Run(e)
	if state.Picked != "/abs/path/results.csv" {
		t.Errorf("state.Picked = %q, want path", state.Picked)
	}
	if !state.Force {
		t.Error("state.Force = false after 'U'; want true")
	}
	if cmd == nil || !isQuitMsg(cmd()) {
		t.Errorf("force-upload Run = %v, want tea.QuitMsg", cmd)
	}
}

func TestUploadAction_HasConfirm(t *testing.T) {
	t.Parallel()
	up := findAction(t, BuildActions(&State{}), "u")
	if !up.Confirm {
		t.Error("upload.Confirm = false; want true")
	}
	if up.ConfirmPrompt == nil {
		t.Fatal("upload.ConfirmPrompt = nil")
	}
}

func TestUploadAction_ConfirmPrompt_FileVsFolder(t *testing.T) {
	t.Parallel()
	up := findAction(t, BuildActions(&State{}), "u")

	file := Entry{Name: "results.csv"}
	title, _ := up.ConfirmPrompt(file)
	if !strings.Contains(title, "upload") || !strings.Contains(title, "results.csv") || strings.Contains(title, "results.csv/") {
		t.Errorf("file upload prompt = %q, want 'upload results.csv' without trailing slash", title)
	}

	dir := Entry{Name: "myrepo", Dir: true}
	dtitle, _ := up.ConfirmPrompt(dir)
	if !strings.Contains(dtitle, "myrepo/") {
		t.Errorf("folder upload prompt = %q, want trailing slash on folder name", dtitle)
	}
}

func TestForceUploadAction_HasConfirm(t *testing.T) {
	t.Parallel()
	force := findAction(t, BuildActions(&State{}), "U")
	if !force.Confirm {
		t.Error("force-upload.Confirm = false; want true")
	}
	if force.ConfirmPrompt == nil {
		t.Fatal("force-upload.ConfirmPrompt = nil")
	}
}

func TestForceUploadAction_ConfirmPrompt_MentionsOverwrite(t *testing.T) {
	t.Parallel()
	force := findAction(t, BuildActions(&State{}), "U")
	title, _ := force.ConfirmPrompt(Entry{Name: "results.csv"})
	if !strings.Contains(title, "overwrite") {
		t.Errorf("force-upload prompt = %q, want it to mention overwrite (it's what distinguishes U from u)", title)
	}
}

func TestToggleHiddenAction_NoConfirm(t *testing.T) {
	t.Parallel()
	// Toggle hidden is a view-state flip, not a transfer/destructive
	// action — explicitly out of the "confirm everything that isn't
	// motion" scope. Pin it so it can't drift.
	toggle := findAction(t, BuildActions(&State{}), ".")
	if toggle.Confirm {
		t.Error("toggle-hidden should not require confirmation; it's an in-pane view flip")
	}
}

func TestToggleHiddenAction_Run_FlipsAndRefreshes(t *testing.T) {
	t.Parallel()
	state := &State{}
	actions := BuildActions(state)
	toggle := findAction(t, actions, ".")

	cmd := toggle.Run(Entry{Name: "any"})
	if !state.ShowHidden() {
		t.Error("ShowHidden not flipped after first toggle Run")
	}
	if cmd == nil {
		t.Fatal("toggle.Run returned nil Cmd")
	}
	// Cmd is a tea.Batch of (StatusMsg, RefreshMsg); we don't dispatch
	// it here (batches are runtime-specific). We just confirm state
	// changed and a Cmd came back.
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func entryNames(es []browser.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.(Entry).Name
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func findAction(t *testing.T, actions []browser.Action, keyStr string) browser.Action {
	t.Helper()
	for _, a := range actions {
		for _, k := range a.Key.Keys() {
			if k == keyStr {
				return a
			}
		}
	}
	t.Fatalf("no action bound to %q", keyStr)
	return browser.Action{}
}

// isQuitMsg matches tea.QuitMsg without importing it by name — the
// concrete type is tea.QuitMsg{} (empty struct).
func isQuitMsg(msg tea.Msg) bool {
	_, ok := msg.(tea.QuitMsg)
	return ok
}
