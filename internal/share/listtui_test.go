package share

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
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

func TestFileItem_DescriptionNoDesc(t *testing.T) {
	t.Parallel()
	// Ensure dim style is used for the "no description" case
	_ = ui.TUI // ensure styles are initialized
	item := fileItem{entry: SharedEntry{Type: "file", Size: 512}}
	desc := item.Description()
	if desc == "" {
		t.Error("expected non-empty description even without user description")
	}
}
