package share

import (
	"testing"

	"github.com/sciminds/cli/internal/cloud"
)

// fixture mimics the layout that lives in sciminds/private after seeding the
// python-tutorials tree under ejolly/, with one foreign-owner file mixed in
// so we exercise the multi-user root case.
var fixture = []cloud.ObjectInfo{
	{Key: "ejolly/python-tutorials/data/credit.csv", Size: 22373},
	{Key: "ejolly/python-tutorials/data/example.csv", Size: 119},
	{Key: "ejolly/python-tutorials/notebooks/01-intro.py", Size: 13868},
	{Key: "ejolly/pyproject.toml", Size: 2363},
	{Key: "alice/results.csv", Size: 1024},
}

func TestChildrenAt_Root(t *testing.T) {
	entries := ChildrenAt(fixture, "")
	if len(entries) != 2 {
		t.Fatalf("root children = %d, want 2: %+v", len(entries), entries)
	}
	if !entries[0].IsDir || entries[0].Name != "alice" {
		t.Errorf("root[0] = %+v, want alice/ folder", entries[0])
	}
	if !entries[1].IsDir || entries[1].Name != "ejolly" {
		t.Errorf("root[1] = %+v, want ejolly/ folder", entries[1])
	}
}

func TestChildrenAt_UserFolder_MixesDirsAndFiles(t *testing.T) {
	entries := ChildrenAt(fixture, "ejolly")
	if len(entries) != 2 {
		t.Fatalf("children = %d: %+v", len(entries), entries)
	}
	// Folders sort before files.
	if !entries[0].IsDir || entries[0].Name != "python-tutorials" {
		t.Errorf("[0] should be python-tutorials/, got %+v", entries[0])
	}
	if entries[1].IsDir || entries[1].Name != "pyproject.toml" {
		t.Errorf("[1] should be pyproject.toml, got %+v", entries[1])
	}
	if entries[1].Size != 2363 {
		t.Errorf("[1] size = %d, want 2363", entries[1].Size)
	}
}

func TestChildrenAt_NestedFolder(t *testing.T) {
	entries := ChildrenAt(fixture, "ejolly/python-tutorials")
	if len(entries) != 2 {
		t.Fatalf("children = %d: %+v", len(entries), entries)
	}
	for i, want := range []string{"data", "notebooks"} {
		if !entries[i].IsDir || entries[i].Name != want {
			t.Errorf("[%d] want %s/, got %+v", i, want, entries[i])
		}
	}
}

func TestChildrenAt_FileLevel_AlphabeticalFiles(t *testing.T) {
	entries := ChildrenAt(fixture, "ejolly/python-tutorials/data")
	if len(entries) != 2 {
		t.Fatalf("children = %d", len(entries))
	}
	// credit.csv sorts before example.csv.
	if entries[0].Name != "credit.csv" || entries[0].IsDir {
		t.Errorf("[0] want credit.csv file, got %+v", entries[0])
	}
	if entries[1].Name != "example.csv" || entries[1].IsDir {
		t.Errorf("[1] want example.csv file, got %+v", entries[1])
	}
}

func TestChildrenAt_NonexistentPath_ReturnsEmpty(t *testing.T) {
	entries := ChildrenAt(fixture, "nonexistent")
	if len(entries) != 0 {
		t.Errorf("nonexistent path should be empty, got %+v", entries)
	}
}

func TestChildrenAt_NoDuplicateFolders(t *testing.T) {
	// Two files in the same folder must collapse to one folder entry.
	objs := []cloud.ObjectInfo{
		{Key: "ejolly/data/a.csv"},
		{Key: "ejolly/data/b.csv"},
		{Key: "ejolly/data/c.csv"},
	}
	entries := ChildrenAt(objs, "ejolly")
	if len(entries) != 1 || entries[0].Name != "data" || !entries[0].IsDir {
		t.Errorf("want one data/ folder, got %+v", entries)
	}
}

func TestTreeEntry_Owner(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"ejolly/results.csv", "ejolly"},
		{"ejolly/sub/file.csv", "ejolly"},
		{"file.csv", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := TreeEntry{Key: tc.key}.Owner()
		if got != tc.want {
			t.Errorf("Owner(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func TestParentPath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ejolly/data", "ejolly"},
		{"ejolly", ""},
		{"", ""},
		{"ejolly/data/file.csv", "ejolly/data"},
		{"ejolly/", ""},
	}
	for _, tc := range cases {
		if got := ParentPath(tc.in); got != tc.want {
			t.Errorf("ParentPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
