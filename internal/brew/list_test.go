package brew

import (
	"testing"
)

func TestListModel_ViewAtZeroSize(t *testing.T) {
	t.Parallel()
	m := newListModel(nil)
	_ = m.View() // must not panic before WindowSizeMsg
}

func TestListModel_ViewWithItems(t *testing.T) {
	t.Parallel()
	packages := []PackageInfo{
		{Name: "ripgrep", Desc: "fast grep", Version: "14.1", Type: "formula"},
		{Name: "firefox", Desc: "web browser", Type: "cask"},
	}
	m := newListModel(packages)
	v := m.View()
	if v.Content == "" {
		t.Error("expected non-empty view")
	}
}

func TestMakeListItem_Cask(t *testing.T) {
	t.Parallel()
	item := makeListItem(PackageInfo{Name: "firefox", Type: "cask", Desc: "browser"})
	if item.Title() != "firefox (cask)" {
		t.Errorf("title = %q, want %q", item.Title(), "firefox (cask)")
	}
}

func TestMakeListItem_Formula(t *testing.T) {
	t.Parallel()
	item := makeListItem(PackageInfo{Name: "ripgrep", Type: "formula", Desc: "fast grep"})
	if item.Title() != "ripgrep" {
		t.Errorf("title = %q, want %q", item.Title(), "ripgrep")
	}
}

func TestMakeListItem_FilterValue(t *testing.T) {
	t.Parallel()
	item := makeListItem(PackageInfo{Name: "rg", Desc: "fast grep"})
	if item.FilterValue() != "rg fast grep" {
		t.Errorf("filter = %q, want %q", item.FilterValue(), "rg fast grep")
	}
}
