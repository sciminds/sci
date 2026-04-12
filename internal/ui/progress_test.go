package ui

import (
	"errors"
	"strings"
	"testing"
)

// ── Model unit tests ──────────────��────────────────────────────────────────

func TestProgressModel_Init(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Extracting…")
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a tick Cmd")
	}
}

func TestProgressModel_DoneMsg(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Extracting…")
	updated, cmd := m.Update(progressDoneMsg{err: nil})
	pm := updated.(progressModel)

	if !pm.done {
		t.Error("expected done=true after progressDoneMsg")
	}
	if pm.err != nil {
		t.Errorf("expected nil error, got %v", pm.err)
	}
	if cmd == nil {
		t.Error("expected quit Cmd after progressDoneMsg")
	}
}

func TestProgressModel_DoneMsgWithError(t *testing.T) {
	t.Parallel()
	want := errors.New("docling failed")
	m := newProgressModel("Extracting…")
	updated, _ := m.Update(progressDoneMsg{err: want})
	pm := updated.(progressModel)

	if !pm.done {
		t.Error("expected done=true")
	}
	if !errors.Is(pm.err, want) {
		t.Errorf("got %v, want %v", pm.err, want)
	}
}

func TestProgressModel_UpdateMsg(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Extracting…")
	msg := progressUpdateMsg{
		current:   3,
		total:     10,
		status:    "processing item.pdf",
		lastEvent: "✓ created foo.pdf",
		counters:  map[string]int{"created": 2, "skipped": 1},
	}
	updated, _ := m.Update(msg)
	pm := updated.(progressModel)

	if pm.current != 3 {
		t.Errorf("current = %d, want 3", pm.current)
	}
	if pm.total != 10 {
		t.Errorf("total = %d, want 10", pm.total)
	}
	if pm.status != "processing item.pdf" {
		t.Errorf("status = %q, want %q", pm.status, "processing item.pdf")
	}
	if pm.lastEvent != "✓ created foo.pdf" {
		t.Errorf("lastEvent = %q, want %q", pm.lastEvent, "✓ created foo.pdf")
	}
	if pm.counters["created"] != 2 {
		t.Errorf("counters[created] = %d, want 2", pm.counters["created"])
	}
}

func TestProgressModel_ViewEmptyWhenDone(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Extracting…")
	m.done = true
	v := m.View()
	if v.Content != "" {
		t.Errorf("expected empty view when done, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsTitle(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Extracting library")
	v := m.View()
	if !strings.Contains(v.Content, "Extracting library") {
		t.Errorf("view should contain title, got %q", v.Content)
	}
}

func TestProgressModel_ViewShowsBar(t *testing.T) {
	t.Parallel()
	m := newProgressModel("Working…")
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
	m := newProgressModel("Working…")
	m.total = 10
	m.current = 3
	m.counters = map[string]int{"created": 2, "skipped": 1}
	v := m.View()
	if !strings.Contains(v.Content, "created") {
		t.Errorf("view should show 'created' counter, got %q", v.Content)
	}
}

// ── Quiet-mode integration tests ���─────────────────────���───────────────────

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
