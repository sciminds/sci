package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTickRenderer_SuspendHidesOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTickRenderer(&buf, "Working…")
	r.start()
	time.Sleep(200 * time.Millisecond)

	// Should have rendered something.
	r.mu.Lock()
	got := buf.String()
	r.mu.Unlock()
	if got == "" {
		t.Error("expected spinner output before suspend")
	}

	// Suspend — next ticks should not add spinner frames.
	r.suspend()
	r.mu.Lock()
	buf.Reset()
	r.mu.Unlock()
	time.Sleep(200 * time.Millisecond)

	r.mu.Lock()
	afterSuspend := buf.String()
	r.mu.Unlock()
	if strings.ContainsAny(afterSuspend, "⣾⣽⣻⢿⡿⣟⣯⣷") {
		t.Errorf("spinner should not render frames while suspended, got %q", afterSuspend)
	}

	// Resume — should render again.
	r.resume()
	r.mu.Lock()
	buf.Reset()
	r.mu.Unlock()
	time.Sleep(200 * time.Millisecond)

	r.mu.Lock()
	afterResume := buf.String()
	r.mu.Unlock()
	if afterResume == "" {
		t.Error("expected spinner output after resume")
	}

	r.stop()
}

func TestTickRenderer_SetTitleClearsStatus(t *testing.T) {
	var buf bytes.Buffer
	r := newTickRenderer(&buf, "Phase 1")

	r.setStatus("detail")
	r.mu.Lock()
	if r.status != "detail" {
		t.Errorf("status = %q, want %q", r.status, "detail")
	}
	r.mu.Unlock()

	r.setTitle("Phase 2")
	r.mu.Lock()
	if r.title != "Phase 2" {
		t.Errorf("title = %q, want %q", r.title, "Phase 2")
	}
	if r.status != "" {
		t.Errorf("status should be cleared after title change, got %q", r.status)
	}
	r.mu.Unlock()
}

func TestTickRenderer_StopClearsOutput(t *testing.T) {
	var buf bytes.Buffer
	r := newTickRenderer(&buf, "Loading…")
	r.start()
	time.Sleep(200 * time.Millisecond)
	r.stop()

	// After stop, the output should end with a clear sequence (cursor up + clear line + \r).
	got := buf.String()
	if !strings.HasSuffix(got, "\r") {
		t.Errorf("stop should end with cursor at column 0, output ends with %q", got[max(0, len(got)-20):])
	}
	// Should contain cursor-up escape to erase the spinner line.
	if !strings.Contains(got, "\033[A") {
		t.Error("stop should move cursor up to clear spinner line")
	}
}

func TestTickRenderer_RendersAboveCursor(t *testing.T) {
	var buf bytes.Buffer
	r := newTickRenderer(&buf, "Installing…")
	// Render two frames manually.
	r.render()
	r.render()

	got := buf.String()
	// First render: frame + newline (no cursor-up needed).
	// Second render: cursor-up + clear + frame + newline.
	// Count cursor-up sequences — should have exactly 1 (from second render).
	ups := strings.Count(got, "\033[A")
	if ups != 1 {
		t.Errorf("expected 1 cursor-up (from second render), got %d", ups)
	}
}

func TestTickRenderer_ProgressMode(t *testing.T) {
	var buf bytes.Buffer
	r := newProgressRenderer(&buf, "Downloading…", func(cur, tot int64) string {
		return "  half"
	})
	r.setProgress(50, 100)
	r.start()
	time.Sleep(200 * time.Millisecond)
	r.stop()

	got := buf.String()
	if !strings.Contains(got, "Downloading") {
		t.Errorf("progress output should contain title, got %q", got)
	}
}
