package extract

import (
	"math"
	"regexp"
	"strconv"
	"time"
)

// EventKind classifies a docling log line.
type EventKind int

const (
	// EventProcessing indicates docling started processing a document.
	EventProcessing EventKind = iota
	// EventFinished indicates docling finished converting a document.
	EventFinished
	// EventOutput indicates docling wrote an output file.
	EventOutput
	// EventFailed indicates a document failed to convert.
	EventFailed
	// EventSummary is the final "Processed N docs, of which M failed" line.
	EventSummary
)

// DoclingEvent is a parsed progress event from docling's stderr.
type DoclingEvent struct {
	Kind       EventKind
	Document   string        // filename or path, depending on the event
	Duration   time.Duration // only for EventFinished
	OutputPath string        // only for EventOutput
	Processed  int           // only for EventSummary
	Failed     int           // only for EventSummary
}

var (
	reProcessing = regexp.MustCompile(`Processing document (.+)$`)
	reFinished   = regexp.MustCompile(`Finished converting document (.+) in ([0-9.]+) sec\.$`)
	reOutput     = regexp.MustCompile(`writing (?:Markdown|JSON|HTML) output to (.+)$`)
	reFailed     = regexp.MustCompile(`Document (.+) failed to convert\.$`)
	reSummary    = regexp.MustCompile(`Processed (\d+) docs, of which (\d+) failed$`)
)

// ParseDoclingEvent extracts a structured event from a single docling
// stderr log line. Returns nil for lines that don't match any known
// progress pattern (model loading, HTTP requests, weight progress bars, etc.).
func ParseDoclingEvent(line string) *DoclingEvent {
	if m := reFinished.FindStringSubmatch(line); m != nil {
		dur := parseDuration(m[2])
		return &DoclingEvent{Kind: EventFinished, Document: m[1], Duration: dur}
	}
	if m := reProcessing.FindStringSubmatch(line); m != nil {
		return &DoclingEvent{Kind: EventProcessing, Document: m[1]}
	}
	if m := reOutput.FindStringSubmatch(line); m != nil {
		return &DoclingEvent{Kind: EventOutput, OutputPath: m[1]}
	}
	if m := reFailed.FindStringSubmatch(line); m != nil {
		return &DoclingEvent{Kind: EventFailed, Document: m[1]}
	}
	if m := reSummary.FindStringSubmatch(line); m != nil {
		processed, _ := strconv.Atoi(m[1])
		failed, _ := strconv.Atoi(m[2])
		return &DoclingEvent{Kind: EventSummary, Processed: processed, Failed: failed}
	}
	return nil
}

// parseDuration parses "16.63" into 16.63 seconds as time.Duration.
// Rounds to millisecond precision to avoid float→int jitter.
func parseDuration(s string) time.Duration {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	ms := math.Round(f * 1000)
	return time.Duration(ms) * time.Millisecond
}
