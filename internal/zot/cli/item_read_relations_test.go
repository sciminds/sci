package cli

// Tests for the related-items block on `sci zot item read` — the CLI-level
// proof that relations survive the whole path (SQLite → local.Item → the
// --json envelope and the human renderer). Reuses seedOrientDB /
// withOrientConfig from info_orient_test.go, where KEY1 ↔ KEY2 are related.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestItemRead_JSONCarriesRelations(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "KEY1")
	if err != nil {
		t.Fatalf("item read KEY1: %v\n%s", err, string(out))
	}
	var got local.Item
	if err := json.Unmarshal(unwrapData(t, out), &got); err != nil {
		t.Fatalf("parse: %v\n%s", err, string(out))
	}
	if got.Relations == nil {
		t.Fatal("relations absent from item read --json; KEY1 is related to KEY2")
	}
	if len(got.Relations.Related) != 1 || got.Relations.Related[0] != "KEY2" {
		t.Errorf("Related = %v, want [KEY2]", got.Relations.Related)
	}
	if title := got.Relations.Titles["KEY2"]; title != "Paper Two" {
		t.Errorf("Titles[KEY2] = %q, want %q", title, "Paper Two")
	}
}

// The whole point of the feature: you see what an item is related to
// without knowing `zot link list` exists.
func TestItemRead_HumanShowsRelatedBlock(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--library", "personal", "item", "read", "KEY1")
	if err != nil {
		t.Fatalf("item read KEY1: %v\n%s", err, string(out))
	}
	for _, want := range []string{"related", "KEY2", "Paper Two"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in:\n%s", want, string(out))
		}
	}
}

// An item with no relations must render (and serialize) exactly as before.
func TestItemRead_UnrelatedItemUnchanged(t *testing.T) {
	withOrientConfig(t)

	out, err := runItemRead(t, "--json", "--library", "personal", "item", "read", "KEY3")
	if err != nil {
		t.Fatalf("item read KEY3: %v\n%s", err, string(out))
	}
	if strings.Contains(string(out), "relations") {
		t.Errorf("relations key present on an unrelated item:\n%s", string(out))
	}
}
