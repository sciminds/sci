package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/brew"
	"github.com/sciminds/cli/internal/uikit"
)

var testEntries = []brew.BrewfileEntry{
	{Name: "bat", Type: "brew", Spec: "bat", Line: `brew "bat"`},
	{Name: "obsidian", Type: "cask", Spec: "obsidian", Line: `cask "obsidian"`},
	{Name: "made-up-tool", Type: "uv", Spec: "made-up-tool", Line: `uv "made-up-tool"`},
}

func TestOptionalToolOptions_ValueIsName(t *testing.T) {
	t.Parallel()
	opts := optionalToolOptions(testEntries, false)
	if len(opts) != len(testEntries) {
		t.Fatalf("got %d options, want %d", len(opts), len(testEntries))
	}
	for i, e := range testEntries {
		if opts[i].Value != e.Name {
			t.Errorf("option %d value = %q, want %q (must map back to the entry name)", i, opts[i].Value, e.Name)
		}
	}
}

func TestOptionalToolOptions_LabelCarriesDescription(t *testing.T) {
	t.Parallel()
	opts := optionalToolOptions(testEntries, false)
	// bat has a known description → label includes name + " — " + desc.
	batLabel := opts[0].Key
	if !strings.HasPrefix(batLabel, "bat — ") {
		t.Errorf("bat label = %q, want it to start with %q", batLabel, "bat — ")
	}
	if !strings.Contains(batLabel, toolDescs["bat"]) {
		t.Errorf("bat label = %q, want it to contain the description", batLabel)
	}
}

func TestOptionalToolOptions_UnknownToolFallsBackToBareName(t *testing.T) {
	t.Parallel()
	// made-up-tool has no description → no em-dash, just the name (+ type tag).
	opts := optionalToolOptions(testEntries, false)
	label := opts[2].Key
	if strings.Contains(label, "—") {
		t.Errorf("undescribed tool label = %q, want no em-dash separator", label)
	}
	if !strings.HasPrefix(label, "made-up-tool") {
		t.Errorf("label = %q, want it to start with the tool name", label)
	}
}

func TestOptionalToolOptions_MixedViewTagsType(t *testing.T) {
	t.Parallel()
	// The mixed catalog tags each row with its type so apps and CLI tools are
	// distinguishable at a glance.
	opts := optionalToolOptions(testEntries, false)
	if !strings.Contains(opts[1].Key, "(cask)") {
		t.Errorf("obsidian label = %q, want a (cask) type tag in mixed view", opts[1].Key)
	}
}

func TestOptionalToolOptions_AppsViewDropsTypeTag(t *testing.T) {
	t.Parallel()
	// The apps view is all casks, so the redundant type tag is omitted.
	casks := []brew.BrewfileEntry{{Name: "obsidian", Type: "cask", Spec: "obsidian"}}
	opts := optionalToolOptions(casks, true)
	if strings.Contains(opts[0].Key, "(cask)") {
		t.Errorf("apps-view label = %q, should not carry a (cask) tag", opts[0].Key)
	}
}

func TestPickOptionalTools_QuietReturnsFormQuiet(t *testing.T) {
	uikit.SetQuiet(true)
	defer uikit.SetQuiet(false)

	got, err := pickOptionalTools(testEntries, false)
	if !errors.Is(err, uikit.ErrFormQuiet) {
		t.Errorf("pickOptionalTools in quiet mode should return ErrFormQuiet, got: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil selection in quiet mode, got: %v", got)
	}
}
