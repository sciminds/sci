package cloudbrowse

// cloudbrowse_test.go — full-loop teatest coverage for the bucket browser.
// The app/ unit tests cover the Provider, Entry rendering, and each
// Action's predicates/Run in isolation; here we drive the real tea.Model
// through key → Update → View.
//
// Action paths are deliberately no-network: the delete Run shells out to
// `hf` and there's no fake-client seam, so we exercise the two gates that
// fire *before* any network call — Allowed (foreign-owner rejection) and
// Confirm (the two-press modal, then abort). A successful delete would
// need a cloud.Client interface; out of scope here.

import (
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/tui/cloudbrowse/app"
	"github.com/sciminds/cli/internal/tuitest"
)

const (
	testTermW = 80
	testTermH = 24
	testWait  = 5 * time.Second
	testFinal = 8 * time.Second
)

// browseFixture mirrors the app-test fixture: two owners (alice, ejolly),
// nested folders, files at varying depths. ChildrenAt sorts folders-first
// then alphabetically, so at any level the cursor positions below are
// deterministic.
//
//	(root)            alice/        ejolly/
//	ejolly/           python-tutorials/   pyproject.toml
//	ejolly/python-tutorials/   data/   notebooks/
//	alice/            results.csv
var browseFixture = []cloud.ObjectInfo{
	{Key: "ejolly/python-tutorials/data/credit.csv", Size: 1000},
	{Key: "ejolly/python-tutorials/notebooks/01-intro.py", Size: 500},
	{Key: "ejolly/pyproject.toml", Size: 100},
	{Key: "alice/results.csv", Size: 200},
}

// startBrowse starts a teatest program over a fresh copy of the fixture
// and returns the provider so tests can assert the listing was (not)
// pruned by querying it directly rather than scraping the view.
func startBrowse(t *testing.T) (*teatest.TestModel, *app.Provider) {
	t.Helper()
	p := app.NewProvider(slices.Clone(browseFixture), cloud.NewClient("ejolly", cloud.DefaultOrg, cloud.BucketPrivate))
	tm := teatest.NewTestModel(t, newModel(p), teatest.WithInitialTermSize(testTermW, testTermH))
	t.Cleanup(func() { _ = tm.Quit() })
	return tm, p
}

// Thin aliases over the shared tuitest helpers.
func sendKey(tm *teatest.TestModel, s string)      { tuitest.SendKey(tm, s) }
func sendSpecial(tm *teatest.TestModel, code rune) { tuitest.SendSpecial(tm, code) }
func waitOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	tuitest.WaitFor(t, tm, substr, testWait)
}

// finalModel sends ctrl+c (a quit key) and returns the final model. Only
// safe when no confirm modal is active — ctrl+c routed into an open huh
// form aborts the modal instead of quitting the program.
func finalModel(t *testing.T, tm *teatest.TestModel) model {
	return tuitest.Final[model](t, tm, testFinal)
}

func hasKey(objs []cloud.ObjectInfo, key string) bool {
	return slices.ContainsFunc(objs, func(o cloud.ObjectInfo) bool { return o.Key == key })
}

func TestTeatest_Navigate_DescendAndAscend(t *testing.T) {
	tm, _ := startBrowse(t)

	waitOutput(t, tm, "ejolly")   // root: alice/, ejolly/
	sendSpecial(tm, tea.KeyDown)  // cursor alice → ejolly
	sendSpecial(tm, tea.KeyEnter) // descend into ejolly
	// Gate on the breadcrumb path, not a list item: the breadcrumb is set
	// atomically with the listing + cursor reset in handleChildren, so it's
	// an authoritative "level loaded and ready for the next key" signal that
	// no transient frame can spoof.
	waitOutput(t, tm, "/ ejolly")
	sendSpecial(tm, tea.KeyEnter) // descend into python-tutorials (idx 0)
	waitOutput(t, tm, "/ python-tutorials")
	sendSpecial(tm, tea.KeyBackspace) // ascend back to ejolly

	fm := finalModel(t, tm)
	if got := fm.inner.Path(); got != "ejolly" {
		t.Errorf("cwd after ascend = %q, want ejolly", got)
	}
}

func TestTeatest_DeleteForeignFile_Rejected(t *testing.T) {
	tm, p := startBrowse(t)

	waitOutput(t, tm, "ejolly")
	sendSpecial(tm, tea.KeyEnter) // cursor on alice/ (idx 0) → descend
	waitOutput(t, tm, "/ alice")  // breadcrumb: descended into alice
	sendKey(tm, "x")              // alice's file → Allowed rejects before any network
	waitOutput(t, tm, "cannot delete @alice")

	_ = finalModel(t, tm)
	if got := len(p.Objects()); got != len(browseFixture) {
		t.Errorf("objects = %d after rejected delete, want %d (nothing pruned)", got, len(browseFixture))
	}
}

func TestTeatest_DeleteOwnFile_ConfirmThenAbortKeepsObject(t *testing.T) {
	tm, p := startBrowse(t)

	waitOutput(t, tm, "ejolly")
	sendSpecial(tm, tea.KeyDown)  // cursor alice → ejolly
	sendSpecial(tm, tea.KeyEnter) // descend into ejolly
	waitOutput(t, tm, "/ ejolly") // breadcrumb: descended; cursor reset to idx 0
	sendSpecial(tm, tea.KeyDown)  // cursor python-tutorials → pyproject.toml
	sendKey(tm, "x")              // own file → confirm modal opens
	waitOutput(t, tm, "Are you sure you want to delete pyproject.toml")
	sendSpecial(tm, tea.KeyEscape) // abort the modal (synchronous; Run never fires)

	_ = finalModel(t, tm)
	if !hasKey(p.Objects(), "ejolly/pyproject.toml") {
		t.Error("pyproject.toml pruned after aborted delete; want it kept")
	}
	if got := len(p.Objects()); got != len(browseFixture) {
		t.Errorf("objects = %d after abort, want %d", got, len(browseFixture))
	}
}
