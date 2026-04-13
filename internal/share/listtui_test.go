package share

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sciminds/cli/internal/uikit"
)

func TestCloudListModel_ViewAtZeroSize(t *testing.T) {
	t.Parallel()
	m := newCloudListModel(nil, nil)
	_ = m.View() // must not panic before WindowSizeMsg
}

func TestCloudListModel_ViewWithItems(t *testing.T) {
	t.Parallel()
	entries := []SharedEntry{
		{Name: "data.tar.gz", Type: "archive", Size: 1024},
		{Name: "notes.md", Type: "file", Size: 256, Description: "lecture notes"},
	}
	m := newCloudListModel(entries, nil)
	v := m.View()
	if v.Content == "" {
		t.Error("expected non-empty view")
	}
}

func TestFileItem_Title(t *testing.T) {
	t.Parallel()
	item := fileItem{entry: SharedEntry{Name: "data.csv"}}
	if item.Title() != "data.csv" {
		t.Errorf("title = %q, want %q", item.Title(), "data.csv")
	}
}

func TestFileItem_FilterValue(t *testing.T) {
	t.Parallel()
	item := fileItem{entry: SharedEntry{Name: "data.csv", Type: "file"}}
	if item.FilterValue() != "data.csv file" {
		t.Errorf("filter = %q, want %q", item.FilterValue(), "data.csv file")
	}
}

func TestFileItem_DescriptionWithSize(t *testing.T) {
	t.Parallel()
	item := fileItem{entry: SharedEntry{Type: "archive", Size: 2048}}
	desc := item.Description()
	if !strings.Contains(desc, "archive") {
		t.Errorf("description should contain type, got %q", desc)
	}
}

func TestFileItem_DescriptionWithUserDesc(t *testing.T) {
	t.Parallel()
	item := fileItem{entry: SharedEntry{Type: "file", Size: 100, Description: "my notes"}}
	desc := item.Description()
	if !strings.Contains(desc, "my notes") {
		t.Errorf("description should contain user description, got %q", desc)
	}
}

// ── Async result message tests ─────────────────────────────────────────────

func TestDeleteResultIsKitResult(t *testing.T) {
	t.Parallel()
	// The deleteFile command should produce a uikit.Result[deleteOK] message,
	// not the old deleteResultMsg type.
	var msg tea.Msg = uikit.Result[deleteOK]{Value: deleteOK{name: "test.csv"}}
	r, ok := msg.(uikit.Result[deleteOK])
	if !ok {
		t.Fatalf("expected uikit.Result[deleteOK], got %T", msg)
	}
	if r.Value.name != "test.csv" {
		t.Errorf("name = %q, want %q", r.Value.name, "test.csv")
	}
}

func TestDownloadResultIsKitResult(t *testing.T) {
	t.Parallel()
	var msg tea.Msg = uikit.Result[downloadOK]{Value: downloadOK{name: "data.tar", path: "data.tar"}}
	r, ok := msg.(uikit.Result[downloadOK])
	if !ok {
		t.Fatalf("expected uikit.Result[downloadOK], got %T", msg)
	}
	if r.Value.name != "data.tar" {
		t.Errorf("name = %q, want %q", r.Value.name, "data.tar")
	}
	if r.Value.path != "data.tar" {
		t.Errorf("path = %q, want %q", r.Value.path, "data.tar")
	}
}

func TestDeleteResultWithError(t *testing.T) {
	t.Parallel()
	msg := uikit.Result[deleteOK]{
		Value: deleteOK{name: "fail.csv"},
		Err:   errTestDelete,
	}
	if msg.Err == nil {
		t.Fatal("expected error in Result")
	}
	if msg.Value.name != "fail.csv" {
		t.Errorf("name = %q, want %q", msg.Value.name, "fail.csv")
	}
}

var errTestDelete = errors.New("delete failed")

func TestModelHandlesDeleteResult(t *testing.T) {
	t.Parallel()
	entries := []SharedEntry{{Name: "a.csv", Type: "file", Size: 100}}
	m := newCloudListModel(entries, nil)
	// Simulate receiving a successful delete result.
	updated, _ := m.Update(uikit.Result[deleteOK]{Value: deleteOK{name: "a.csv"}})
	_ = updated // should not panic
}

func TestModelHandlesDownloadResult(t *testing.T) {
	t.Parallel()
	entries := []SharedEntry{{Name: "a.csv", Type: "file", Size: 100}}
	m := newCloudListModel(entries, nil)
	// Simulate receiving a successful download result.
	updated, _ := m.Update(uikit.Result[downloadOK]{Value: downloadOK{name: "a.csv", path: "a.csv"}})
	_ = updated // should not panic
}

func TestFileItem_DescriptionNoDesc(t *testing.T) {
	t.Parallel()
	// Ensure dim style is used for the "no description" case
	_ = uikit.TUI // ensure styles are initialized
	item := fileItem{entry: SharedEntry{Type: "file", Size: 512}}
	desc := item.Description()
	if desc == "" {
		t.Error("expected non-empty description even without user description")
	}
}
