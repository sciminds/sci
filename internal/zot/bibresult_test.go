package zot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot/bib"
)

func TestBibResult_HumanSurfacesUnresolved(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "biblatex", OutPath: "refs.bib", Stats: ExportStats{Total: 2, Pinned: 1, Synthesized: 1}},
		Files:      []string{"draft.md"},
		References: 3,
		Resolved:   2,
		Unresolved: []bib.Unresolved{
			{Ref: bib.Ref{Raw: "@ghost2020", Kind: bib.KindCitekey, Value: "ghost2020"}, Reason: "no match"},
		},
	}
	h := r.Human()
	for _, want := range []string{"refs.bib", "3 reference(s)", "2 resolved, 1 unresolved", "@ghost2020", "no match"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestBibResult_HumanCleanWhenFullyResolved(t *testing.T) {
	t.Parallel()
	r := BibResult{
		Export:     LibraryExportResult{Format: "biblatex", OutPath: "refs.bib", Stats: ExportStats{Total: 1, Pinned: 1}},
		Files:      []string{"draft.md"},
		References: 1,
		Resolved:   1,
	}
	if h := r.Human(); strings.Contains(h, "unresolved (") {
		t.Errorf("Human() shows unresolved block with none present:\n%s", h)
	}
}

// TestBibResult_CarriesNoVerdictField pins the shape after `bib --verify`
// retired: the unresolved list is the whole answer, and a `verified` key in
// the JSON would promise a classification nothing produces any more.
func TestBibResult_CarriesNoVerdictField(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(BibResult{})
	for i := range typ.NumField() {
		if name := typ.Field(i).Name; name == "Verified" {
			t.Errorf("BibResult still carries a %s field", name)
		}
	}
	blob, err := json.Marshal(BibResult{Unresolved: []bib.Unresolved{{Ref: bib.Ref{Raw: "@Nope2020"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "verified") {
		t.Errorf("BibResult JSON still mentions a verdict: %s", blob)
	}
}
