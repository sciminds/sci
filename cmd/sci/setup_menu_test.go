package main

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/sciminds/cli/internal/tuitest"
)

func sampleSetupStatuses() []domainStatus {
	return []domainStatus{
		{Key: "lab", Title: "Lab storage", Configured: false, Summary: "not configured"},
		{Key: "zot", Title: "Zotero", Configured: true, Summary: "user 123"},
	}
}

// TestSetupMenu_OpenWithLRecordsPick proves the menu rides the shared keymap:
// `l` opens the highlighted row (it is not a huh prompt anymore) and the chosen
// domain key is carried out to the caller.
func TestSetupMenu_OpenWithLRecordsPick(t *testing.T) {
	m := newSetupMenu(sampleSetupStatuses())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "l") // open the highlighted row
	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if !fm.pickedOK || fm.picked != "lab" {
		t.Errorf("after `l`: picked=%q ok=%v, want lab/true", fm.picked, fm.pickedOK)
	}
}

// TestSetupMenu_QuitWithQ proves q exits the menu without choosing a tool.
func TestSetupMenu_QuitWithQ(t *testing.T) {
	m := newSetupMenu(sampleSetupStatuses())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	tuitest.WaitFor(t, tm, "Lab storage", 2*time.Second)

	tuitest.SendKey(tm, "q")
	fm := tuitest.Final[*setupMenuModel](t, tm, 3*time.Second)
	if fm.pickedOK {
		t.Error("q should quit the menu without a pick")
	}
}
