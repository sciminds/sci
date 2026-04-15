package app

import (
	"reflect"
	"slices"
	"testing"

	"github.com/sciminds/cli/internal/lab"
)

// helper: a backend pre-seeded with a small directory tree.
func sampleBackend() *fakeBackend {
	b := newFakeBackend()
	b.seedListing("/labs/sciminds",
		lab.Entry{Name: "data", IsDir: true},
		lab.Entry{Name: "scripts", IsDir: true},
		lab.Entry{Name: "README.md"},
	)
	b.seedListing("/labs/sciminds/data",
		lab.Entry{Name: "exp1", IsDir: true},
		lab.Entry{Name: "exp2", IsDir: true},
		lab.Entry{Name: "results.csv"},
	)
	b.seedListing("/labs/sciminds/data/exp1",
		lab.Entry{Name: "raw.bin"},
	)
	b.seedSize("/labs/sciminds/data/exp1", 1024)
	b.seedSize("/labs/sciminds/data/results.csv", 512)
	return b
}

func newTestModel(t *testing.T) (*Model, *fakeBackend) {
	t.Helper()
	b := sampleBackend()
	m := NewModel(&lab.Config{User: "alice"}, b)
	return m, b
}

func TestNewModel_StartsAtRoot(t *testing.T) {
	m, _ := newTestModel(t)
	if m.cwd != lab.ReadRoot {
		t.Errorf("cwd = %q, want %q", m.cwd, lab.ReadRoot)
	}
	if m.screen != screenBrowse {
		t.Errorf("screen = %v, want screenBrowse", m.screen)
	}
	if m.SelectedCount() != 0 {
		t.Errorf("expected no selections initially")
	}
}

func TestModel_ToggleSelect(t *testing.T) {
	m, _ := newTestModel(t)
	m.entries = []lab.Entry{
		{Name: "a"},
		{Name: "b", IsDir: true},
	}
	m.cursor = 0
	m.toggleSelectAtCursor()
	if !m.isSelected("/labs/sciminds/a") {
		t.Errorf("entry a should be selected")
	}
	if m.SelectedCount() != 1 {
		t.Errorf("count = %d, want 1", m.SelectedCount())
	}
	// Toggle off.
	m.toggleSelectAtCursor()
	if m.isSelected("/labs/sciminds/a") {
		t.Errorf("entry a should be deselected")
	}
}

func TestModel_SelectionPersistsAcrossDirs(t *testing.T) {
	m, _ := newTestModel(t)
	m.selectPath("/labs/sciminds/data/results.csv")
	m.selectPath("/labs/sciminds/scripts/run.sh")
	got := m.SelectedPaths()
	slices.Sort(got)
	want := []string{
		"/labs/sciminds/data/results.csv",
		"/labs/sciminds/scripts/run.sh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SelectedPaths\n  got:  %v\n  want: %v", got, want)
	}
}

func TestModel_SelectedPathsIsAlphabetical(t *testing.T) {
	m, _ := newTestModel(t)
	m.selectPath("/labs/sciminds/z")
	m.selectPath("/labs/sciminds/a")
	m.selectPath("/labs/sciminds/m")
	got := m.SelectedPaths()
	want := []string{
		"/labs/sciminds/a",
		"/labs/sciminds/m",
		"/labs/sciminds/z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected alphabetical, got %v", got)
	}
}

func TestModel_BreadcrumbAtRoot(t *testing.T) {
	m, _ := newTestModel(t)
	got := m.Breadcrumb()
	if got != "sciminds" {
		t.Errorf("Breadcrumb at root = %q, want %q", got, "sciminds")
	}
}

func TestModel_BreadcrumbInSubdir(t *testing.T) {
	m, _ := newTestModel(t)
	m.cwd = "/labs/sciminds/data/exp1"
	got := m.Breadcrumb()
	want := "sciminds / data / exp1"
	if got != want {
		t.Errorf("Breadcrumb = %q, want %q", got, want)
	}
}

func TestModel_CanAscendFromSubdir(t *testing.T) {
	m, _ := newTestModel(t)
	m.cwd = "/labs/sciminds/data"
	if !m.canAscend() {
		t.Errorf("should be able to ascend from %s", m.cwd)
	}
}

func TestModel_CannotAscendFromRoot(t *testing.T) {
	m, _ := newTestModel(t)
	if m.canAscend() {
		t.Errorf("should not ascend above ReadRoot")
	}
}

func TestModel_PathForEntry(t *testing.T) {
	m, _ := newTestModel(t)
	got := m.pathFor(lab.Entry{Name: "data", IsDir: true})
	want := "/labs/sciminds/data"
	if got != want {
		t.Errorf("pathFor = %q, want %q", got, want)
	}
}
