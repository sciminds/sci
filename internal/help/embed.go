package help

import (
	"embed"
	"io/fs"
	"slices"
	"strings"

	"github.com/samber/lo"
)

//go:embed casts
var castFS embed.FS

// loadCast reads a cast file from the embedded filesystem.
func loadCast(name string) ([]byte, error) {
	return castFS.ReadFile("casts/" + name)
}

// hasCast reports whether a cast file exists in the embedded filesystem.
func hasCast(name string) bool {
	_, err := fs.Stat(castFS, "casts/"+name)
	return err == nil
}

// castsWithPrefix returns every embedded cast named "<prefix>-*.cast", sorted.
// Used when a picker row has no cast of its own but its children do — see
// castsFor in entries.go.
func castsWithPrefix(prefix string) []string {
	entries, err := fs.ReadDir(castFS, "casts")
	if err != nil {
		return nil
	}
	names := lo.FilterMap(entries, func(e fs.DirEntry, _ int) (string, bool) {
		n := e.Name()
		return n, !e.IsDir() &&
			strings.HasPrefix(n, prefix+"-") &&
			strings.HasSuffix(n, ".cast")
	})
	slices.Sort(names)
	return names
}
