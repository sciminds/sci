package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// seedManifests writes n completed key dirs whose manifests carry the
// given per-page rates, cycling through rates in key order.
func seedManifests(t *testing.T, dir string, n int, rates []float64) {
	t.Helper()
	for i := range n {
		key := fmt.Sprintf("KEY%05d", i)
		keyDir := filepath.Join(dir, key)
		if err := os.MkdirAll(keyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		pages := 10
		man := LayoutManifest{
			Key:     key,
			Status:  "success",
			NPages:  pages,
			Secs:    rates[i%len(rates)] * float64(pages),
			PDFPath: "/tmp/" + key + ".pdf",
		}
		raw, err := json.Marshal(man)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(keyDir, "result.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCalibrateSecondsPerPage_MedianOverCorpus(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// 100 manifests at 1, 2, 3, 4, 5 s/page → median 3.
	seedManifests(t, dir, 100, []float64{1, 2, 3, 4, 5})

	cal, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: dir})
	if !ok {
		t.Fatal("expected a calibration from 100 manifests")
	}
	if cal.Samples != 100 {
		t.Errorf("samples = %d, want 100", cal.Samples)
	}
	if cal.SecondsPerPage != 3 {
		t.Errorf("seconds/page = %v, want 3 (the median)", cal.SecondsPerPage)
	}
}

// The median, not the mean, is the point: one 55-minute OCR book must
// not drag the estimate for a queue of ordinary papers.
func TestCalibrateSecondsPerPage_MedianResistsOutliers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rates := make([]float64, 0, 50)
	for range 49 {
		rates = append(rates, 2)
	}
	rates = append(rates, 600) // one scanned monograph
	seedManifests(t, dir, 50, rates)

	cal, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: dir})
	if !ok {
		t.Fatal("expected a calibration")
	}
	if cal.SecondsPerPage != 2 {
		t.Errorf("seconds/page = %v, want 2 — the outlier must not move the median", cal.SecondsPerPage)
	}
}

func TestCalibrateSecondsPerPage_TooFewSamples(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedManifests(t, dir, minCalibrationSamples-1, []float64{2})

	if _, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: dir}); ok {
		t.Errorf("want no calibration below %d samples — too thin to beat the device guess", minCalibrationSamples)
	}
}

func TestCalibrateSecondsPerPage_NoCorpus(t *testing.T) {
	t.Parallel()
	if _, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: t.TempDir()}); ok {
		t.Error("empty layout dir must not calibrate")
	}
	if _, ok := CalibrateSecondsPerPage(nil); ok {
		t.Error("nil layout (classic mode) must not calibrate")
	}
	if _, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: filepath.Join(t.TempDir(), "nope")}); ok {
		t.Error("missing layout dir must not calibrate")
	}
}

// Manifests that can't produce a rate are skipped, not counted: a
// zero-page or zero-second record would otherwise divide by zero or
// report an impossibly fast machine.
func TestCalibrateSecondsPerPage_SkipsUnusableManifests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedManifests(t, dir, minCalibrationSamples, []float64{2})

	write := func(key string, man LayoutManifest) {
		t.Helper()
		keyDir := filepath.Join(dir, key)
		if err := os.MkdirAll(keyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(man)
		if err := os.WriteFile(filepath.Join(keyDir, "result.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ZZZZ0001", LayoutManifest{Key: "ZZZZ0001", NPages: 0, Secs: 40})
	write("ZZZZ0002", LayoutManifest{Key: "ZZZZ0002", NPages: 10, Secs: 0})
	// A dir with no manifest at all, and one with unparseable JSON.
	if err := os.MkdirAll(filepath.Join(dir, "ZZZZ0003"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "ZZZZ0004"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ZZZZ0004", "result.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	cal, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: dir})
	if !ok {
		t.Fatal("expected a calibration from the usable manifests")
	}
	if cal.Samples != minCalibrationSamples {
		t.Errorf("samples = %d, want %d — only usable manifests count", cal.Samples, minCalibrationSamples)
	}
	if cal.SecondsPerPage != 2 {
		t.Errorf("seconds/page = %v, want 2", cal.SecondsPerPage)
	}
}

// Reading every manifest in a 4,900-dir corpus on every --plan is a cost
// nobody asked for; the sample is capped and the cap is what bounds it.
func TestCalibrateSecondsPerPage_CapsSampleCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedManifests(t, dir, maxCalibrationSamples+50, []float64{2})

	cal, ok := CalibrateSecondsPerPage(&KeyLayout{Dir: dir})
	if !ok {
		t.Fatal("expected a calibration")
	}
	if cal.Samples != maxCalibrationSamples {
		t.Errorf("samples = %d, want the cap %d", cal.Samples, maxCalibrationSamples)
	}
}

// A calibrated rate must actually reach the ETA — otherwise the whole
// calibration is a number nobody reads.
func TestBuildSurvey_CalibratedRateBeatsDeviceGuess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	items := []BatchItem{
		mkBatchItem("PA", "PDFPA", "a.pdf", "/x/a.pdf", "ha", ActionCreate),
	}
	counter := func(string) (int, error) { return 100, nil }

	in := SurveyInput{
		Items: items, Cache: &MarkdownCache{Dir: filepath.Join(dir, "cache")},
		Pages: counter, Jobs: 1, Device: "mps",
	}
	guessed := BuildSurvey(in)
	if guessed.SecondsPerPage != SecondsPerPage("mps") {
		t.Errorf("uncalibrated rate = %v, want the device guess %v",
			guessed.SecondsPerPage, SecondsPerPage("mps"))
	}
	if guessed.CalibrationSamples != 0 {
		t.Errorf("samples = %d, want 0 when nothing was calibrated", guessed.CalibrationSamples)
	}

	in.SecondsPerPage, in.CalibrationSamples = 3.0, 250
	cal := BuildSurvey(in)
	if cal.SecondsPerPage != 3.0 {
		t.Errorf("calibrated rate = %v, want 3.0", cal.SecondsPerPage)
	}
	if cal.CalibrationSamples != 250 {
		t.Errorf("samples = %d, want 250", cal.CalibrationSamples)
	}
	if cal.ETA != EstimateDurationAt(100, 1, 3.0) {
		t.Errorf("ETA = %v, want %v", cal.ETA, EstimateDurationAt(100, 1, 3.0))
	}
	if cal.ETA <= guessed.ETA {
		t.Error("a slower observed rate must produce a longer ETA than the guess")
	}
}

func TestEstimateDurationAt_UsesRateAndJobs(t *testing.T) {
	t.Parallel()
	// 100 pages at 3 s/page over 2 jobs = 150s.
	got := EstimateDurationAt(100, 2, 3).Seconds()
	if got != 150 {
		t.Errorf("got %vs, want 150s", got)
	}
	// jobs 0 means a single process, not division by zero.
	if got := EstimateDurationAt(100, 0, 3).Seconds(); got != 300 {
		t.Errorf("jobs=0: got %vs, want 300s", got)
	}
}
