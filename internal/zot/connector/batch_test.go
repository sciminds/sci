package connector

// Tests for CollectPaths (path classification + directory walk) and
// ImportBatch (serial orchestration with per-file error tolerance).
// The fakeTransport from import_test.go is reused — both files are in
// package connector so it's directly accessible.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- CollectPaths ---

func TestCollectPaths_singleFile(t *testing.T) {
	t.Parallel()
	path := writeTempPDF(t, "%PDF")
	got, skipped, err := CollectPaths([]string{path})
	if err != nil {
		t.Fatalf("CollectPaths: %v", err)
	}
	if len(got) != 1 || got[0] != path {
		t.Errorf("paths = %v, want [%q]", got, path)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0 (no walk happened)", skipped)
	}
}

func TestCollectPaths_multipleFiles(t *testing.T) {
	t.Parallel()
	a := writeTempPDF(t, "a")
	b := writeTempPDF(t, "b")
	got, _, err := CollectPaths([]string{a, b})
	if err != nil {
		t.Fatalf("CollectPaths: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("paths = %v, want 2 entries", got)
	}
}

func TestCollectPaths_rejectsNonPDFFileArg(t *testing.T) {
	t.Parallel()
	// When the user passes a file explicitly, surface a clear error rather
	// than silently dropping it. Silent skip only applies to the dir walk.
	dir := t.TempDir()
	notPDF := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notPDF, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := CollectPaths([]string{notPDF})
	if err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Fatalf("expected PDF error, got %v", err)
	}
}

func TestCollectPaths_rejectsMixedFileAndDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pdf := writeTempPDF(t, "x")
	_, _, err := CollectPaths([]string{pdf, dir})
	if err == nil || !strings.Contains(err.Error(), "mix") {
		t.Fatalf("expected mixed-args rejection, got %v", err)
	}
}

func TestCollectPaths_rejectsTwoDirs(t *testing.T) {
	t.Parallel()
	d1 := t.TempDir()
	d2 := t.TempDir()
	_, _, err := CollectPaths([]string{d1, d2})
	if err == nil {
		t.Fatal("expected error for two-directory invocation")
	}
}

func TestCollectPaths_directoryWalk(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.pdf"), "a")
	mustWrite(t, filepath.Join(root, "b.PDF"), "b") // case-insensitive
	mustMkdir(t, filepath.Join(root, "sub"))
	mustWrite(t, filepath.Join(root, "sub", "c.pdf"), "c")
	mustWrite(t, filepath.Join(root, "notes.txt"), "x") // counted skip
	mustWrite(t, filepath.Join(root, "image.png"), "y") // counted skip

	got, skipped, err := CollectPaths([]string{root})
	if err != nil {
		t.Fatalf("CollectPaths: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len(paths) = %d (%v), want 3", len(got), got)
	}
	if skipped != 2 {
		t.Errorf("skippedNonPDF = %d, want 2", skipped)
	}
}

func TestCollectPaths_walkSkipsHidden(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "visible.pdf"), "v")
	mustWrite(t, filepath.Join(root, ".hidden.pdf"), "h")
	mustMkdir(t, filepath.Join(root, ".hidden_dir"))
	mustWrite(t, filepath.Join(root, ".hidden_dir", "inside.pdf"), "x")

	got, _, err := CollectPaths([]string{root})
	if err != nil {
		t.Fatalf("CollectPaths: %v", err)
	}
	// Only visible.pdf should be returned.
	if len(got) != 1 || filepath.Base(got[0]) != "visible.pdf" {
		t.Errorf("paths = %v, want only visible.pdf", got)
	}
}

func TestCollectPaths_walkSkipsSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := writeTempPDF(t, "target") // outside root
	mustWrite(t, filepath.Join(root, "real.pdf"), "real")

	link := filepath.Join(root, "linked.pdf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	got, _, err := CollectPaths([]string{root})
	if err != nil {
		t.Fatalf("CollectPaths: %v", err)
	}
	for _, p := range got {
		if filepath.Base(p) == "linked.pdf" {
			t.Errorf("symlinked PDF should be skipped during walk; got %v", got)
		}
	}
	if len(got) != 1 {
		t.Errorf("paths = %v, want only the real PDF", got)
	}
}

func TestCollectPaths_emptyDirErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, _, err := CollectPaths([]string{root})
	if err == nil || !strings.Contains(err.Error(), "no PDFs") {
		t.Fatalf("expected 'no PDFs' error, got %v", err)
	}
}

// --- ImportBatch ---

func TestImportBatch_pingsOnce_evenForManyFiles(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{
		pollFn: func(_ context.Context, _ string) (*RecognizedResp, error) {
			return &RecognizedResp{Recognized: true, Title: "T", ItemType: "journalArticle"}, nil
		},
	}
	paths := []string{
		writeTempPDF(t, "a"),
		writeTempPDF(t, "b"),
		writeTempPDF(t, "c"),
	}
	_, err := ImportBatch(context.Background(), ft, paths, BatchOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("ImportBatch: %v", err)
	}
	if ft.pingCalls != 1 {
		t.Errorf("Ping called %d times, want exactly 1 (per-batch, not per-file)", ft.pingCalls)
	}
	if ft.saveCalls != 3 {
		t.Errorf("Save called %d times, want 3", ft.saveCalls)
	}
}

func TestImportBatch_forcesNoWait_skipsRecognize(t *testing.T) {
	t.Parallel()
	// Batch mode hardcodes NoWait=true regardless of opts. GetRecognizedItem
	// must NOT be called.
	ft := &fakeTransport{}
	paths := []string{writeTempPDF(t, "a"), writeTempPDF(t, "b")}
	res, err := ImportBatch(context.Background(), ft, paths, BatchOptions{Timeout: time.Hour})
	if err != nil {
		t.Fatalf("ImportBatch: %v", err)
	}
	if ft.pollCalls != 0 {
		t.Errorf("GetRecognizedItem calls = %d, want 0 (batch forces NoWait)", ft.pollCalls)
	}
	if res.Imported != 2 || res.Recognized != 0 || res.Failed != 0 {
		t.Errorf("counters = imported:%d recognized:%d failed:%d; want imported:2", res.Imported, res.Recognized, res.Failed)
	}
}

func TestImportBatch_continuesOnPerFileError(t *testing.T) {
	t.Parallel()
	// Second file's Save fails, third should still process. fakeTransport's
	// saveFn is shared across calls, so script behavior by call count.
	var saveN int
	ft := &fakeTransport{
		saveFn: func(_ context.Context, _ io.Reader, _ SaveMeta) (*SaveResp, error) {
			saveN++
			if saveN == 2 {
				return nil, errors.New("desktop choked on this one")
			}
			return &SaveResp{CanRecognize: true}, nil
		},
	}
	paths := []string{writeTempPDF(t, "a"), writeTempPDF(t, "b"), writeTempPDF(t, "c")}
	res, err := ImportBatch(context.Background(), ft, paths, BatchOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("ImportBatch must not error on per-file failure: %v", err)
	}
	if len(res.Items) != 3 {
		t.Errorf("len(items) = %d, want 3 (one per input)", len(res.Items))
	}
	if res.Failed != 1 || res.Imported != 2 {
		t.Errorf("counters = imported:%d failed:%d; want imported:2 failed:1", res.Imported, res.Failed)
	}
	if res.Items[1].Err == "" {
		t.Error("item[1].Err should describe the failure")
	}
	if res.Items[2].Err != "" {
		t.Error("item[2] should have processed successfully after item[1] failed")
	}
}

func TestImportBatch_invokesProgressHooks(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{}
	paths := []string{writeTempPDF(t, "a"), writeTempPDF(t, "b")}

	var startCalls, doneCalls int
	var lastTotal int
	_, err := ImportBatch(context.Background(), ft, paths, BatchOptions{
		Timeout: time.Second,
		OnStart: func(_, total int, _ string) {
			startCalls++
			lastTotal = total
		},
		OnDone: func(_, _ int, _ ItemResult) {
			doneCalls++
		},
	})
	if err != nil {
		t.Fatalf("ImportBatch: %v", err)
	}
	if startCalls != 2 || doneCalls != 2 {
		t.Errorf("hooks: start=%d done=%d, want 2/2", startCalls, doneCalls)
	}
	if lastTotal != 2 {
		t.Errorf("OnStart total = %d, want 2", lastTotal)
	}
}

func TestImportBatch_pingFailureAbortsImmediately(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{pingErr: ErrDesktopUnreachable}
	_, err := ImportBatch(context.Background(), ft, []string{writeTempPDF(t, "a")}, BatchOptions{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected error when Ping fails")
	}
	if !errors.Is(err, ErrDesktopUnreachable) {
		t.Errorf("err = %v, want wrapping ErrDesktopUnreachable", err)
	}
	if ft.saveCalls != 0 {
		t.Error("must not attempt any uploads after Ping failed")
	}
}

func TestImportBatch_ctxCancelBetweenFilesStopsLoop(t *testing.T) {
	t.Parallel()
	// Cancel after the first file completes; second file should not be
	// uploaded. The result still carries the first file's outcome.
	ctx, cancel := context.WithCancel(context.Background())
	var saveN int
	ft := &fakeTransport{
		saveFn: func(_ context.Context, _ io.Reader, _ SaveMeta) (*SaveResp, error) {
			saveN++
			if saveN == 1 {
				cancel()
			}
			return &SaveResp{CanRecognize: false}, nil
		},
	}
	paths := []string{writeTempPDF(t, "a"), writeTempPDF(t, "b"), writeTempPDF(t, "c")}
	res, err := ImportBatch(ctx, ft, paths, BatchOptions{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected ctx error on cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(res.Items) != 1 || ft.saveCalls != 1 {
		t.Errorf("processed %d items / %d saves; cancel must stop further work", len(res.Items), ft.saveCalls)
	}
}

// --- helpers ---

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
