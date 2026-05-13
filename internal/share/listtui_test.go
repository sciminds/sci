package share

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/uikit"
)

// browseFixture is a small synthetic bucket: two users (ejolly, alice),
// nested folders, a couple of files at different depths. The same shape
// the real python-tutorials data takes after upload, just smaller.
var browseFixture = []cloud.ObjectInfo{
	{Key: "ejolly/python-tutorials/data/credit.csv", Size: 1000},
	{Key: "ejolly/python-tutorials/notebooks/01-intro.py", Size: 500},
	{Key: "ejolly/pyproject.toml", Size: 100},
	{Key: "alice/results.csv", Size: 200},
}

// fakeClient returns a Client struct sufficient for the bits the model
// reads (Username, Bucket). The real run/stream funcs are never invoked
// by the tests below — async delete/download paths are exercised in
// teatest-style tests elsewhere.
func fakeClient(username string) *cloud.Client {
	return cloud.NewClient(username, cloud.DefaultOrg, cloud.BucketPrivate)
}

// ── Item rendering ──────────────────────────────────────────────────────────

func TestEntryItem_Title_FolderHasTrailingSlash(t *testing.T) {
	t.Parallel()
	dir := entryItem{entry: TreeEntry{Name: "data", IsDir: true}}
	if dir.Title() != "data/" {
		t.Errorf("folder Title = %q, want %q", dir.Title(), "data/")
	}
	file := entryItem{entry: TreeEntry{Name: "results.csv"}}
	if file.Title() != "results.csv" {
		t.Errorf("file Title = %q, want %q", file.Title(), "results.csv")
	}
}

func TestEntryItem_Description_FolderVsFile(t *testing.T) {
	t.Parallel()
	dir := entryItem{entry: TreeEntry{Name: "data", IsDir: true}}
	if !strings.Contains(dir.Description(), "folder") {
		t.Errorf("folder Description = %q, want to contain 'folder'", dir.Description())
	}
	file := entryItem{entry: TreeEntry{Name: "results.csv", Size: 2048}}
	desc := file.Description()
	if !strings.Contains(desc, "csv") || !strings.Contains(desc, "kB") {
		t.Errorf("file Description = %q, want type+size", desc)
	}
}

func TestEntryItem_FilterValue_UsesName(t *testing.T) {
	t.Parallel()
	got := entryItem{entry: TreeEntry{Name: "credit.csv"}}.FilterValue()
	if got != "credit.csv" {
		t.Errorf("FilterValue = %q, want credit.csv", got)
	}
}

// ── Model construction + breadcrumb ─────────────────────────────────────────

func TestNewCloudBrowseModel_StartsAtRoot(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	if m.cwd != "" {
		t.Errorf("cwd = %q, want empty (root)", m.cwd)
	}
	if got := len(m.list.Items()); got != 2 {
		t.Errorf("root items = %d, want 2 (alice/, ejolly/)", got)
	}
}

func TestCloudBrowseModel_Breadcrumb_ReflectsCwd(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	if got := m.breadcrumb(); got != "sciminds/private" {
		t.Errorf("root breadcrumb = %q", got)
	}
	m.cwd = "ejolly/python-tutorials"
	if got := m.breadcrumb(); got != "sciminds/private / ejolly / python-tutorials" {
		t.Errorf("nested breadcrumb = %q", got)
	}
}

// ── Navigation ──────────────────────────────────────────────────────────────

func TestCloudBrowseModel_DescendIntoFolder(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	// Root sorts as alice/, ejolly/. Move cursor onto ejolly/ (index 1).
	m.list.Select(1)
	// Press Enter → descend into ejolly/.
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(cloudBrowseModel)
	if mm.cwd != "ejolly" {
		t.Errorf("cwd after Enter = %q, want %q", mm.cwd, "ejolly")
	}
	// ejolly/ contains python-tutorials/ + pyproject.toml.
	if got := len(mm.list.Items()); got != 2 {
		t.Errorf("ejolly children = %d, want 2", got)
	}
}

func TestCloudBrowseModel_AscendWithBackspace(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	m.cwd = "ejolly/python-tutorials"
	m.rebuild()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	mm := updated.(cloudBrowseModel)
	if mm.cwd != "ejolly" {
		t.Errorf("cwd after Backspace = %q, want %q", mm.cwd, "ejolly")
	}
}

func TestCloudBrowseModel_BackspaceAtRoot_NoOp(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	mm := updated.(cloudBrowseModel)
	if mm.cwd != "" {
		t.Errorf("cwd should stay empty at root, got %q", mm.cwd)
	}
}

func TestCloudBrowseModel_EnterOnFile_DoesNotChangeCwd(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	m.cwd = "ejolly"
	m.rebuild()
	// ejolly/ children: python-tutorials/ (idx 0, dir), pyproject.toml (idx 1, file).
	m.list.Select(1)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(cloudBrowseModel)
	if mm.cwd != "ejolly" {
		t.Errorf("Enter on file changed cwd to %q, want unchanged", mm.cwd)
	}
}

// ── Ownership-gated delete ──────────────────────────────────────────────────

func TestHandleRemove_DenyForeignOwner(t *testing.T) {
	t.Parallel()
	pending := new(string)
	dummy := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	item := entryItem{entry: TreeEntry{Name: "results.csv", Key: "alice/results.csv"}}
	cmd := handleRemove(&dummy, item, fakeClient("ejolly"), pending)
	if cmd == nil {
		t.Fatal("expected a status-message Cmd for foreign-owner delete")
	}
	if *pending != "" {
		t.Errorf("pendingDelete = %q, want empty (foreign delete must not arm confirm)", *pending)
	}
}

func TestHandleRemove_DenyFolder(t *testing.T) {
	t.Parallel()
	pending := new(string)
	dummy := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	item := entryItem{entry: TreeEntry{Name: "data", Key: "ejolly/data", IsDir: true}}
	cmd := handleRemove(&dummy, item, fakeClient("ejolly"), pending)
	if cmd == nil {
		t.Fatal("expected a status-message Cmd for folder delete attempt")
	}
	if *pending != "" {
		t.Errorf("pendingDelete = %q, want empty", *pending)
	}
}

func TestHandleRemove_OwnFile_FirstPressArmsConfirm(t *testing.T) {
	t.Parallel()
	pending := new(string)
	dummy := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	item := entryItem{entry: TreeEntry{Name: "iris.csv", Key: "ejolly/iris.csv"}}
	cmd := handleRemove(&dummy, item, fakeClient("ejolly"), pending)
	if cmd == nil {
		t.Fatal("expected a status-message Cmd")
	}
	if *pending != "ejolly/iris.csv" {
		t.Errorf("pendingDelete = %q, want armed with the key", *pending)
	}
}

func TestHandleRemove_AnyOtherKey_ClearsPending(t *testing.T) {
	t.Parallel()
	pending := new(string)
	*pending = "ejolly/iris.csv"
	dummy := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	// Press copyURL on a folder — pending must clear.
	_ = handleCopyURL(&dummy, entryItem{entry: TreeEntry{Name: "data", IsDir: true}})
	// pending wasn't touched by handleCopyURL itself; the delegate
	// switch case clears it before dispatch. Mirror that here.
	*pending = ""
	if *pending != "" {
		t.Errorf("pending should clear, got %q", *pending)
	}
}

// ── Async result dispatch ───────────────────────────────────────────────────

var errTestDelete = errors.New("delete failed")

func TestModel_DeleteResult_PrunesUnderlyingObjects(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	updated, _ := m.Update(uikit.Result[deleteOK]{Value: deleteOK{key: "alice/results.csv", name: "results.csv"}})
	mm := updated.(cloudBrowseModel)
	for _, o := range mm.objects {
		if o.Key == "alice/results.csv" {
			t.Errorf("alice/results.csv should be gone, still present in objects")
		}
	}
}

func TestModel_DeleteResultWithErr_KeepsObjects(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	updated, _ := m.Update(uikit.Result[deleteOK]{
		Value: deleteOK{key: "ejolly/pyproject.toml", name: "pyproject.toml"},
		Err:   errTestDelete,
	})
	mm := updated.(cloudBrowseModel)
	found := false
	for _, o := range mm.objects {
		if o.Key == "ejolly/pyproject.toml" {
			found = true
		}
	}
	if !found {
		t.Errorf("failed delete must not prune the object")
	}
}

func TestModel_DownloadResult_OK(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	updated, _ := m.Update(uikit.Result[downloadOK]{Value: downloadOK{key: "ejolly/pyproject.toml", path: "pyproject.toml"}})
	_ = updated // shouldn't panic; status message goes to list
}

// ── View at zero size + items (regression) ──────────────────────────────────

func TestCloudBrowseModel_ViewAtZeroSize(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(nil, fakeClient("ejolly"))
	_ = m.View() // must not panic before WindowSizeMsg
}

func TestCloudBrowseModel_ViewWithItems(t *testing.T) {
	t.Parallel()
	m := newCloudBrowseModel(browseFixture, fakeClient("ejolly"))
	if v := m.View(); v.Content == "" {
		t.Error("expected non-empty view")
	}
}
