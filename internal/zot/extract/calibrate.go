package extract

// calibrate.go — deriving the --plan ETA's per-page rate from the
// extractions already on disk.
//
// [SecondsPerPage] is a hardcoded order-of-magnitude guess per device.
// Measured against a 4,900-document layout corpus it was off by 1.5×
// (1.5 s/page assumed, 2.26 s/page observed on mps), and the error is
// hardware-specific — nothing about the constant can track the machine
// the user actually runs docling on. Every finished extraction already
// records its own cost in result.json, so the corpus can answer the
// question the constant guesses at.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	// minCalibrationSamples is how many usable manifests the corpus must
	// hold before its median displaces the device guess. Below this the
	// sample says more about which papers were extracted first than
	// about the machine.
	minCalibrationSamples = 25

	// maxCalibrationSamples bounds the read cost. The corpus grows
	// without limit and --plan must stay cheap; a few hundred documents
	// pin a median far tighter than an ETA needs.
	maxCalibrationSamples = 300
)

// Calibration is a per-page docling rate observed from finished
// extractions, with the sample size behind it so callers can say where
// the number came from instead of presenting it as fact.
type Calibration struct {
	// SecondsPerPage is the median over Samples documents.
	SecondsPerPage float64
	Samples        int
}

// CalibrateSecondsPerPage derives the median seconds-per-page from the
// result.json manifests in layout's corpus. ok is false when there is
// no corpus, or too few usable manifests to beat the device guess —
// callers then fall back to [SecondsPerPage].
//
// The median, not the mean: per-page cost is roughly flat across
// document sizes (1.5–2.6 s/page across every page bucket in the live
// corpus) but the tail is enormous — a scanned monograph runs 68 s/page
// and would drag a mean into uselessness for the papers that make up
// most queues.
//
// Sampling is the first maxCalibrationSamples dirs in directory order,
// not the most recent. That keeps the estimate stable run to run (a
// jittering ETA reads as a broken one) and costs one readdir; the rate
// is flat enough across the corpus that a recency-weighted sample would
// not buy accuracy worth a stat of every dir.
func CalibrateSecondsPerPage(layout *KeyLayout) (Calibration, bool) {
	if layout == nil || layout.Dir == "" {
		return Calibration{}, false
	}
	entries, err := os.ReadDir(layout.Dir)
	if err != nil {
		return Calibration{}, false
	}

	rates := make([]float64, 0, min(len(entries), maxCalibrationSamples))
	for _, e := range entries {
		if len(rates) >= maxCalibrationSamples {
			break
		}
		if !e.IsDir() {
			continue
		}
		rate, ok := manifestRate(filepath.Join(layout.Dir, e.Name(), "result.json"))
		if !ok {
			continue
		}
		rates = append(rates, rate)
	}
	if len(rates) < minCalibrationSamples {
		return Calibration{}, false
	}
	return Calibration{SecondsPerPage: median(rates), Samples: len(rates)}, true
}

// manifestRate reads one result.json and returns its seconds-per-page.
// Unreadable, unparseable, and zero-valued manifests are all "no rate"
// rather than errors: a corpus this old accumulates partial records,
// and one bad file must not cost the whole calibration.
func manifestRate(path string) (float64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var man LayoutManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return 0, false
	}
	if man.NPages <= 0 || man.Secs <= 0 {
		return 0, false
	}
	return man.Secs / float64(man.NPages), true
}

// median returns the middle value of xs, averaging the two middle
// values for an even count. It sorts a copy — callers keep their order.
func median(xs []float64) float64 {
	s := slices.Sorted(slices.Values(xs))
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// EstimateDurationAt converts a page count into a wall-clock estimate at
// an explicit per-page rate — the calibrated sibling of
// [EstimateDuration]. jobs ≤ 1 means a single docling process.
func EstimateDurationAt(totalPages, jobs int, secondsPerPage float64) time.Duration {
	secs := float64(totalPages) * secondsPerPage / float64(max(jobs, 1))
	return time.Duration(secs * float64(time.Second))
}
