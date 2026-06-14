package main

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/tuitest"
)

func sampleSetupEntries() []setupEntry {
	return []setupEntry{
		{
			key: "lab", title: "Lab storage",
			status: func() (bool, string) { return false, "not configured" },
			fields: func() []fieldRow { return []fieldRow{{label: "user", value: "(not set)"}} },
		},
		{
			key: "zot", title: "Zotero",
			status: func() (bool, string) { return true, "user 123" },
			fields: func() []fieldRow {
				return []fieldRow{{label: "api_key", value: "k"}, {label: "user_id", value: "123"}}
			},
		},
	}
}

// TestSetupMenu_OpenWithLDrillsIntoFields proves `l` on a tool descends into
// its field list rather than picking — the top level navigates, it does not
// launch setup directly.
func TestSetupMenu_OpenWithLDrillsIntoFields(t *testing.T) {
	m := newSetupMenu(sampleSetupEntries())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "l") // drill into Lab storage
	// The drilled-in frame shows both the "select any field" title and the
	// "user" field row; wait on the field label (a single wait — consecutive
	// WaitFors consume the same output stream).
	tuitest.WaitFor(t, tm, "user", 2*time.Second)

	tuitest.SendKey(tm, "q")
	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if fm.pickedOK {
		t.Error("drilling in then quitting should not record a pick")
	}
}

// TestSetupMenu_OpenFieldRecordsPick proves that opening any field row records
// the tool key so the caller launches that tool's wizard.
func TestSetupMenu_OpenFieldRecordsPick(t *testing.T) {
	m := newSetupMenu(sampleSetupEntries())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "l") // drill into Lab storage
	tuitest.WaitFor(t, tm, "select any field", 2*time.Second)
	tuitest.SendKey(tm, "l") // open the highlighted field → launch setup

	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if !fm.pickedOK || fm.picked != "lab" {
		t.Errorf("after opening a field: picked=%q ok=%v, want lab/true", fm.picked, fm.pickedOK)
	}
}

// TestSetupMenu_BackReturnsToTools proves esc/h backs out of a tool's fields to
// the tool list instead of quitting.
func TestSetupMenu_BackReturnsToTools(t *testing.T) {
	m := newSetupMenu(sampleSetupEntries())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "l") // drill into Lab storage
	tuitest.WaitFor(t, tm, "select any field", 2*time.Second)
	tuitest.SendKey(tm, "h") // back out to the tool list
	tuitest.WaitFor(t, tm, "pick a tool", 2*time.Second)

	tuitest.SendKey(tm, "q")
	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if fm.pickedOK {
		t.Error("backing out then quitting should not record a pick")
	}
}

// TestSetupMenu_QuitWithQ proves q exits the menu without choosing a tool.
func TestSetupMenu_QuitWithQ(t *testing.T) {
	m := newSetupMenu(sampleSetupEntries())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "q")
	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if fm.pickedOK {
		t.Error("q should quit the menu without a pick")
	}
}
