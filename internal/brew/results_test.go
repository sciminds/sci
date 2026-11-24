package brew

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateResult_ShowsHeldBack(t *testing.T) {
	t.Parallel()
	got := UpdateResult{
		Ignored: []OutdatedPackage{{Type: TypeCask, Name: "libreoffice", InstalledVersion: "26.8.0", CurrentVersion: "26.8.3"}},
		Pinned:  []OutdatedPackage{{Type: TypeFormula, Name: "jq", InstalledVersion: "1.7", CurrentVersion: "1.8", Pinned: true}},
	}.Human()
	for _, want := range []string{"1 ignored", "libreoffice", "26.8.3", "1 pinned", "jq"} {
		if !strings.Contains(got, want) {
			t.Errorf("Human() = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "Everything is up to date") {
		t.Errorf("Human() = %q, claims everything is up to date", got)
	}
}

func TestUpdateResult_JSONEmptyLists(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(UpdateResult{}.JSON())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ignored":[]`, `"pinned":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON = %s, want it to contain %s", data, want)
		}
	}
}
