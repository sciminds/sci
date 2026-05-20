package app

import (
	"strings"
	"testing"
)

// TestBoundedWidthSampleASCII checks the byte-cap kicks in for long ASCII
// values and leaves short values untouched.
func TestBoundedWidthSampleASCII(t *testing.T) {
	short := "alice"
	if got := boundedWidthSample(short); got != short {
		t.Errorf("short string changed: got %q want %q", got, short)
	}
	long := strings.Repeat("x", naturalWidthSampleBytes*4)
	got := boundedWidthSample(long)
	if len(got) != naturalWidthSampleBytes {
		t.Errorf("long string not capped: got len=%d want %d", len(got), naturalWidthSampleBytes)
	}
}

// TestBoundedWidthSampleUTF8 verifies that the cap doesn't leave a partial
// UTF-8 sequence at the boundary — lipgloss.Width would mis-measure a
// dangling continuation byte.
func TestBoundedWidthSampleUTF8(t *testing.T) {
	// Build a string whose byte at naturalWidthSampleBytes-1 is the middle
	// of a 3-byte rune (CJK chars are 3 bytes in UTF-8).
	prefix := strings.Repeat("a", naturalWidthSampleBytes-1)
	value := prefix + "中" + strings.Repeat("b", 100)
	got := boundedWidthSample(value)
	if got[len(got)-1]&0xC0 == 0x80 {
		t.Fatalf("boundedWidthSample left a UTF-8 continuation byte at end: %x", got[len(got)-1])
	}
	if len(got) >= naturalWidthSampleBytes {
		t.Fatalf("expected truncation to drop the partial rune, got len=%d", len(got))
	}
}

// TestComputeNaturalWidthsCappedByLongCell verifies that a single ridiculous
// cell value (mimicking a JSON-serialised FLOAT[] embedding) doesn't blow
// out the column's natural width — the cap is enforced at the
// boundedWidthSample boundary so downstream column-layout costs stay bounded.
func TestComputeNaturalWidthsCappedByLongCell(t *testing.T) {
	specs := []columnSpec{{Title: "embedding", Min: 4}}
	rows := [][]cell{{{Value: strings.Repeat("x", 50_000)}}}
	widths := computeNaturalWidths(specs, rows, func(i int) int { return i })
	if widths[0] > naturalWidthSampleBytes {
		t.Errorf("natural width should be bounded by the sample cap, got %d", widths[0])
	}
	if widths[0] < specs[0].Min {
		t.Errorf("natural width should respect Min, got %d", widths[0])
	}
}
