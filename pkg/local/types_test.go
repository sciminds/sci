package local

import (
	"regexp"
	"slices"
	"testing"
)

// TestNonBibliographicTypesMatchSQL pins the Go and SQL statements of the
// same rule together. They are two forms because two planes ask it — a
// query filters at the source, an exporter filters rows it was handed —
// and a type added to one and not the other is exactly the silent gap that
// put annotations into the exported .bib.
func TestNonBibliographicTypesMatchSQL(t *testing.T) {
	t.Parallel()
	quoted := regexp.MustCompile(`'([^']+)'`)
	var fromSQL []string
	for _, m := range quoted.FindAllStringSubmatch(hygieneItemTypeFilter, -1) {
		fromSQL = append(fromSQL, m[1])
	}
	if !slices.Equal(slices.Sorted(slices.Values(fromSQL)), slices.Sorted(slices.Values(nonBibliographicTypes))) {
		t.Errorf("nonBibliographicTypes = %v, but hygieneItemTypeFilter excludes %v", nonBibliographicTypes, fromSQL)
	}
}

func TestIsBibliographic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		itemType string
		want     bool
	}{
		{"journalArticle", true},
		{"book", true},
		{"webpage", true}, // citable, even though it isn't a paper
		{"note", false},
		{"attachment", false},
		{"annotation", false},
	} {
		if got := IsBibliographic(tc.itemType); got != tc.want {
			t.Errorf("IsBibliographic(%q) = %v, want %v", tc.itemType, got, tc.want)
		}
	}
}
