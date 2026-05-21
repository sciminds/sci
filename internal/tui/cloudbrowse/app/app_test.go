package app

// app_test.go — cloudbrowse-specific tests. The browser primitive's own
// behaviour (navigation, two-press confirm, refresh, etc.) is covered
// in internal/uikit/browser. Here we cover the parts unique to the
// cloud wiring:
//   - Entry rendering (folder slash, file type/size description)
//   - Provider.Breadcrumb shape
//   - Action predicates (folder/foreign-owner rejection)
//   - Provider.Remove pruning the underlying slice

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/share"
	"github.com/sciminds/cli/internal/uikit/browser"
)

// browseFixture is a small synthetic bucket: two users (ejolly, alice),
// nested folders, files at different depths. Same shape as the original
// listtui test fixture so any behavioural regressions surface here.
var browseFixture = []cloud.ObjectInfo{
	{Key: "ejolly/python-tutorials/data/credit.csv", Size: 1000},
	{Key: "ejolly/python-tutorials/notebooks/01-intro.py", Size: 500},
	{Key: "ejolly/pyproject.toml", Size: 100},
	{Key: "alice/results.csv", Size: 200},
}

func fakeClient(username string) *cloud.Client {
	return cloud.NewClient(username, cloud.DefaultOrg, cloud.BucketPrivate)
}

// ── Entry rendering ─────────────────────────────────────────────────────────

func TestEntry_Title_FolderHasTrailingSlash(t *testing.T) {
	t.Parallel()
	dir := Entry{T: share.TreeEntry{Name: "data", IsDir: true}}
	if dir.Title() != "data/" {
		t.Errorf("folder Title = %q, want %q", dir.Title(), "data/")
	}
	file := Entry{T: share.TreeEntry{Name: "results.csv"}}
	if file.Title() != "results.csv" {
		t.Errorf("file Title = %q, want %q", file.Title(), "results.csv")
	}
}

func TestEntry_Description_FolderVsFile(t *testing.T) {
	t.Parallel()
	dir := Entry{T: share.TreeEntry{Name: "data", IsDir: true}}
	if !strings.Contains(dir.Description(), "folder") {
		t.Errorf("folder Description = %q, want to contain 'folder'", dir.Description())
	}
	file := Entry{T: share.TreeEntry{Name: "results.csv", Size: 2048}}
	desc := file.Description()
	if !strings.Contains(desc, "csv") || !strings.Contains(desc, "kB") {
		t.Errorf("file Description = %q, want type+size", desc)
	}
}

func TestEntry_FilterValue_UsesName(t *testing.T) {
	t.Parallel()
	got := Entry{T: share.TreeEntry{Name: "credit.csv"}}.FilterValue()
	if got != "credit.csv" {
		t.Errorf("FilterValue = %q, want credit.csv", got)
	}
}

// ── Provider ────────────────────────────────────────────────────────────────

func TestProvider_Breadcrumb(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	if got := p.Breadcrumb(""); got != "sciminds/private" {
		t.Errorf("root breadcrumb = %q", got)
	}
	if got := p.Breadcrumb("ejolly/python-tutorials"); got != "sciminds/private / ejolly / python-tutorials" {
		t.Errorf("nested breadcrumb = %q", got)
	}
}

func TestProvider_ChildrenAtRoot(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	msg := p.Children("")()
	cm, ok := msg.(browser.ChildrenMsg)
	if !ok {
		t.Fatalf("Children emitted %T, want ChildrenMsg", msg)
	}
	if len(cm.Entries) != 2 {
		t.Errorf("root entries = %d, want 2 (alice, ejolly)", len(cm.Entries))
	}
	for _, e := range cm.Entries {
		if !e.IsDir() {
			t.Errorf("root entry %q is not a dir; want all dirs at root", e.Path())
		}
	}
}

func TestProvider_Remove_PrunesObject(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	p.Remove("alice/results.csv")
	for _, o := range p.Objects() {
		if o.Key == "alice/results.csv" {
			t.Errorf("Remove did not prune alice/results.csv")
		}
	}
	// Other objects unaffected.
	if got := len(p.Objects()); got != 3 {
		t.Errorf("objects after remove = %d, want 3", got)
	}
}

func TestProvider_Parent_StaysAtRoot(t *testing.T) {
	t.Parallel()
	p := NewProvider(nil, fakeClient("ejolly"))
	if got := p.Parent(""); got != "" {
		t.Errorf("Parent(\"\") = %q, want \"\"", got)
	}
	if got := p.Parent("ejolly"); got != "" {
		t.Errorf("Parent(ejolly) = %q, want \"\"", got)
	}
	if got := p.Parent("ejolly/data"); got != "ejolly" {
		t.Errorf("Parent(ejolly/data) = %q, want ejolly", got)
	}
}

// ── Action predicates ───────────────────────────────────────────────────────

func TestDeleteAction_AppliesTo_RejectsFolders(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	del := findAction(t, actions, "x")
	dir := Entry{T: share.TreeEntry{Name: "data", IsDir: true, Key: "ejolly/data"}}
	if del.AppliesTo(dir) {
		t.Error("delete AppliesTo true for folder; want false")
	}
}

func TestDeleteAction_Allowed_RejectsForeignOwner(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	del := findAction(t, actions, "x")
	foreign := Entry{T: share.TreeEntry{Name: "results.csv", Key: "alice/results.csv"}}
	ok, reason := del.Allowed(foreign)
	if ok {
		t.Error("delete Allowed=true for foreign owner; want false")
	}
	if !strings.Contains(reason, "alice") {
		t.Errorf("reason = %q, want it to mention the owner", reason)
	}
}

func TestDeleteAction_Allowed_AcceptsOwn(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	del := findAction(t, actions, "x")
	own := Entry{T: share.TreeEntry{Name: "pyproject.toml", Key: "ejolly/pyproject.toml"}}
	ok, _ := del.Allowed(own)
	if !ok {
		t.Error("delete Allowed=false for own file; want true")
	}
}

func TestCopyURLAction_AppliesTo_RejectsFolders(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	c := findAction(t, actions, "c")
	dir := Entry{T: share.TreeEntry{Name: "data", IsDir: true}}
	if c.AppliesTo(dir) {
		t.Error("copy-url AppliesTo=true for folder; want false")
	}
}

func TestCopyURLAction_Allowed_PrivateBucket(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	c := findAction(t, actions, "c")
	noURL := Entry{T: share.TreeEntry{Name: "x.csv", Key: "ejolly/x.csv"}}
	ok, reason := c.Allowed(noURL)
	if ok {
		t.Error("copy-url Allowed=true on private bucket; want false")
	}
	if !strings.Contains(reason, "private bucket") {
		t.Errorf("reason = %q, want it to mention private bucket", reason)
	}
}

func TestDownloadAction_AppliesTo_AllEntries(t *testing.T) {
	t.Parallel()
	p := NewProvider(browseFixture, fakeClient("ejolly"))
	actions := BuildActions(p)
	d := findAction(t, actions, "d")
	if d.AppliesTo != nil {
		t.Error("download.AppliesTo should be nil (applies to all)")
	}
}

// findAction returns the Action whose key binding contains keyStr,
// failing the test if none match.
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
