package share

// tree.go — derive hierarchical (folder + file) views from the flat list of
// keys returned by `hf buckets ls -R`. HF stores objects in a flat namespace
// where "/" is just a character in the key; the tree structure is purely a
// client-side projection. We do that projection here so both `sci cloud ls`
// (plain) and `sci cloud browse` (TUI) can share one navigation model.

import (
	"path"
	"slices"
	"strings"

	"github.com/sciminds/cli/internal/cloud"
)

// TreeEntry is one navigable child at a given bucket path. For folders,
// Size/URL are zero and Key omits the trailing slash. For files, all fields
// come from the underlying ObjectInfo.
type TreeEntry struct {
	Name         string // basename (no slash)
	Key          string // full bucket key (file) or prefix without trailing / (folder)
	IsDir        bool
	Size         int64
	URL          string // public-bucket file URL; empty for folders and private files
	LastModified string
}

// Owner returns the first path component of Key — the per-user folder
// segment used to gate destructive actions like delete. Empty if Key has
// no "/" at all (a top-level object with no owner prefix).
func (e TreeEntry) Owner() string {
	if i := strings.Index(e.Key, "/"); i != -1 {
		return e.Key[:i]
	}
	return ""
}

// ChildrenAt returns the immediate folder + file children under cwd. Pass
// "" for the bucket root. cwd has no trailing slash.
//
// Folders are synthesized from any key whose remainder (after stripping the
// cwd prefix) still contains a "/" — multiple files under one folder collapse
// to a single TreeEntry. Result is sorted folders-first, alphabetically within
// each group.
func ChildrenAt(objects []cloud.ObjectInfo, cwd string) []TreeEntry {
	cwdPrefix := ""
	if cwd != "" {
		cwdPrefix = cwd + "/"
	}

	folders := map[string]bool{}
	var files []TreeEntry

	for _, obj := range objects {
		if !strings.HasPrefix(obj.Key, cwdPrefix) {
			continue
		}
		rest := obj.Key[len(cwdPrefix):]
		if rest == "" {
			continue
		}
		if slash := strings.Index(rest, "/"); slash != -1 {
			folders[rest[:slash]] = true
			continue
		}
		files = append(files, TreeEntry{
			Name:         rest,
			Key:          obj.Key,
			Size:         obj.Size,
			URL:          obj.URL,
			LastModified: obj.LastModified,
		})
	}

	out := make([]TreeEntry, 0, len(folders)+len(files))
	for f := range folders {
		out = append(out, TreeEntry{
			Name:  f,
			Key:   path.Join(cwd, f),
			IsDir: true,
		})
	}
	out = append(out, files...)

	slices.SortStableFunc(out, func(a, b TreeEntry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// ParentPath returns the parent directory of a bucket path, with any
// trailing slash stripped. Empty for the bucket root or a top-level key.
func ParentPath(p string) string {
	p = strings.TrimSuffix(p, "/")
	i := strings.LastIndex(p, "/")
	if i == -1 {
		return ""
	}
	return p[:i]
}
