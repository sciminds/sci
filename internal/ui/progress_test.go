package ui

import (
	"errors"
	"strings"
	"testing"
)

// ── Model unit tests ──────────────────────────────────────��─────────────────

func TestProgressModel_Init(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Extracting…", true)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a tick Cmd")
	}
}

func TestProgressModel_DoneMsg(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Extracting…", true)
	updated, cmd := m.Update(doneMsg{err: nil})
	rm := updated.(runnerModel)

	if !rm.done {
		t.Error("expected done=true after doneMsg")
	}
	if rm.err != nil {
		t.Errorf("expected nil error, got %v", rm.err)
	}
	if cmd == nil {
		t.Error("expected quit Cmd after doneMsg")
	}
}

func TestProgressModel_DoneMsgWithError(t *testing.T) {
	t.Parallel()
	want := errors.New("docling failed")
	m := newRunnerModel("Extracting…", true)
	updated, _ := m.Update(doneMsg{err: want})
	rm := updated.(runnerModel)

	if !rm.done {
		t.Error("expected done=true")
	}
	if !errors.Is(rm.err, want) {
		t.Errorf("got %v, want %v", rm.err, want)
	}
}

func TestProgressModel_UpdateMsg(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Extracting…", true)
	msg := progressUpdateMsg{
		current:   3,
		total:     10,
		status:    "processing item.pdf",
		lastEvent: "✓ created foo.pdf",
		counters:  []counterEntry{{Key: "created", Count: 2}, {Key: "skipped", Count: 1}},
	}
	updated, _ := m.Update(msg)
	rm := updated.(runnerModel)

	if rm.current != 3 {
		t.Errorf("current = %d, want 3", rm.current)
	}
	if rm.total != 10 {
		t.Errorf("total = %d, want 10", rm.total)
	}
	if rm.status != "processing item.pdf" {
		t.Errorf("status = %q, want %q", rm.status, "processing item.pdf")
	}
	if rm.lastEvent != "✓ created foo.pdf" {
		t.Errorf("lastEvent = %q, want %q", rm.lastEvent, "✓ created foo.pdf")
	}
	found := false
	for _, c := range rm.counters {
		if c.Key == "created" && c.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("counters should contain created:2, got %v", rm.counters)
	}
}

func TestProgressModel_ViewEmptyWhenDone(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Extracting…", true)
	m.done = true
	v := m.View()
	if v.Content != "" {
		t.Errorf("expected empty view when done, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsTitle(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Extracting library", true)
	v := m.View()
	if !strings.Contains(v.Content, "Extracting library") {
		t.Errorf("view should contain title, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsBar(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Working…", true)
	m.total = 10
	m.current = 5
	m.width = 60
	v := m.View()
	if !strings.Contains(v.Content, "50%") {
		t.Errorf("view should show 50%%, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsCounters(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Working…", true)
	m.total = 10
	m.current = 3
	m.counters = []counterEntry{{Key: "created", Count: 2}, {Key: "skipped", Count: 1}}
	v := m.View()
	if !strings.Contains(v.Content, "created") {
		t.Errorf("view should show 'created' counter, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsUnknownCounters(t *testing.T) {
	t.Parallel()
	m := newRunnerModel("Working…", true)
	m.total = 10
	m.current = 3
	m.counters = []counterEntry{{Key: "patched", Count: 5}}
	v := m.View()
	if !strings.Contains(v.Content, "patched") {
		t.Errorf("view should show 'patched' counter, got %q", v.Content)
	}
}

// ── Counter sort tests ────���─────────────────────────────────────────────────

func TestSortCounters_KnownOrder(t *testing.T) {
	t.Parallel()
	input := []counterEntry{
		{Key: "failed", Count: 1},
		{Key: "created", Count: 3},
		{Key: "skipped", Count: 2},
	}
	got := sortCounters(input)
	want := []string{"created", "skipped", "failed"}
	for i, w := range want {
		if got[i].Key != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Key, w)
		}
	}
}

func TestSortCounters_UnknownAfterKnown(t *testing.T) {
	t.Parallel()
	input := []counterEntry{
		{Key: "zapped", Count: 1},
		{Key: "created", Count: 2},
		{Key: "alpha", Count: 3},
	}
	got := sortCounters(input)
	want := []string{"created", "alpha", "zapped"}
	for i, w := range want {
		if got[i].Key != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Key, w)
		}
	}
}

// ── Quiet-mode integration tests ────────────────────────────────────────────

func TestRunWithProgress_QuietRunsFn(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	ran := false
	err := RunWithProgress("extracting…", func(tr *ProgressTracker) error {
		ran = true
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ran {
		t.Error("fn should have been called")
	}
}

func TestRunWithProgress_QuietReturnsError(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	want := errors.New("docling crashed")
	got := RunWithProgress("extracting…", func(tr *ProgressTracker) error {
		return want
	})

	if !errors.Is(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunWithProgress_QuietPrintsTitleToStderr(t *testing.T) {
	SetQuiet(true)
	defer SetQuiet(false)

	out := captureStderr(t, func() {
		_ = RunWithProgress("Extracting library…", func(tr *ProgressTracker) error {
			return nil
		})
	})

	if !strings.Contains(out, "Extracting library…") {
		t.Errorf("stderr should contain title, got %q", out)
	}
}
