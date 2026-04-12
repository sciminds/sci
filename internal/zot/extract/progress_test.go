package extract

import (
	"testing"
)

func TestParseDoclingEvent_Processing(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:51:24,226	INFO	docling.pipeline.base_pipeline: Processing document 1967-Meehl-theory-testing.pdf`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventProcessing {
		t.Errorf("Kind = %v, want EventProcessing", ev.Kind)
	}
	if ev.Document != "1967-Meehl-theory-testing.pdf" {
		t.Errorf("Document = %q", ev.Document)
	}
}

func TestParseDoclingEvent_Finished(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:51:39,735	INFO	docling.document_converter: Finished converting document 1967-Meehl-theory-testing.pdf in 16.63 sec.`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventFinished {
		t.Errorf("Kind = %v, want EventFinished", ev.Kind)
	}
	if ev.Document != "1967-Meehl-theory-testing.pdf" {
		t.Errorf("Document = %q", ev.Document)
	}
	// Float→Duration has sub-microsecond jitter; compare at ms precision.
	wantMs := int64(16630)
	gotMs := ev.Duration.Milliseconds()
	if gotMs != wantMs {
		t.Errorf("Duration.Milliseconds() = %d, want %d", gotMs, wantMs)
	}
}

func TestParseDoclingEvent_WritingOutput(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:51:39,735	INFO	docling.cli.main: writing Markdown output to /tmp/out/paper.md`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventOutput {
		t.Errorf("Kind = %v, want EventOutput", ev.Kind)
	}
	if ev.OutputPath != "/tmp/out/paper.md" {
		t.Errorf("OutputPath = %q", ev.OutputPath)
	}
}

func TestParseDoclingEvent_Summary(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:52:02,528	INFO	docling.cli.main: Processed 2 docs, of which 0 failed`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventSummary {
		t.Errorf("Kind = %v, want EventSummary", ev.Kind)
	}
	if ev.Processed != 2 {
		t.Errorf("Processed = %d, want 2", ev.Processed)
	}
	if ev.Failed != 0 {
		t.Errorf("Failed = %d, want 0", ev.Failed)
	}
}

func TestParseDoclingEvent_SummaryWithFailures(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:52:02,528	INFO	docling.cli.main: Processed 10 docs, of which 3 failed`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Processed != 10 || ev.Failed != 3 {
		t.Errorf("Processed=%d Failed=%d, want 10/3", ev.Processed, ev.Failed)
	}
}

func TestParseDoclingEvent_FailedDoc(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:50:42,141	WARNING	docling.cli.main: Document /tmp/bad.pdf failed to convert.`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventFailed {
		t.Errorf("Kind = %v, want EventFailed", ev.Kind)
	}
	if ev.Document != "/tmp/bad.pdf" {
		t.Errorf("Document = %q", ev.Document)
	}
}

func TestParseDoclingEvent_Irrelevant(t *testing.T) {
	t.Parallel()
	lines := []string{
		`2026-04-12 03:51:23,114	INFO	docling.models.factories.base_factory: Loading plugin 'docling_defaults'`,
		`Loading weights: 100%|██████████| 770/770 [00:00<00:00, 3439.07it/s]`,
		``,
		`some random text`,
	}
	for _, line := range lines {
		if ev := ParseDoclingEvent(line); ev != nil {
			t.Errorf("expected nil for %q, got %+v", line, ev)
		}
	}
}

func TestParseDoclingEvent_FinishedZeroDuration(t *testing.T) {
	t.Parallel()
	line := `2026-04-12 03:50:42,141	INFO	docling.document_converter: Finished converting document null in 0.00 sec.`
	ev := ParseDoclingEvent(line)
	if ev == nil {
		t.Fatal("expected non-nil event")
	}
	if ev.Kind != EventFinished {
		t.Errorf("Kind = %v, want EventFinished", ev.Kind)
	}
	if ev.Duration != 0 {
		t.Errorf("Duration = %v, want 0", ev.Duration)
	}
}
