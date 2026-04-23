package connector

// Orchestrator tests for Import(): ping + upload + poll loop. These drive the
// branching in import.go — recognized vs. timed-out vs. not-recognizable vs.
// desktop-down — without touching any real HTTP server.
//
// The fake Transport below lets us script call-by-call behavior deterministically;
// the polling loop accepts an injected clock so timeout tests don't have to sleep.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTransport is a Transport double. Each method slot is a func so each
// test scripts the exact behavior it needs; counters let assertions check
// call counts.
type fakeTransport struct {
	mu            sync.Mutex
	pingErr       error
	saveFn        func(ctx context.Context, body io.Reader, meta SaveMeta) (*SaveResp, error)
	pollFn        func(ctx context.Context, sessionID string) (*RecognizedResp, error)
	pingCalls     int
	saveCalls     int
	pollCalls     int
	capturedBytes []byte
	capturedMeta  SaveMeta
}

func (f *fakeTransport) Ping(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingCalls++
	return f.pingErr
}

func (f *fakeTransport) SaveStandaloneAttachment(ctx context.Context, body io.Reader, meta SaveMeta) (*SaveResp, error) {
	f.mu.Lock()
	f.saveCalls++
	f.capturedMeta = meta
	b, _ := io.ReadAll(body)
	f.capturedBytes = b
	fn := f.saveFn
	f.mu.Unlock()
	if fn == nil {
		return &SaveResp{CanRecognize: true}, nil
	}
	// Replay bytes so the real function body (if any) can read them.
	return fn(ctx, nopReader(b), meta)
}

func (f *fakeTransport) GetRecognizedItem(ctx context.Context, sessionID string) (*RecognizedResp, error) {
	f.mu.Lock()
	f.pollCalls++
	fn := f.pollFn
	f.mu.Unlock()
	if fn == nil {
		return &RecognizedResp{}, nil
	}
	return fn(ctx, sessionID)
}

// nopReader exists only so fakeTransport.saveFn can re-read the captured
// bytes if it wants — tests that don't read through the body ignore it.
type nopReaderImpl struct {
	b []byte
	i int
}

func nopReader(b []byte) io.Reader { return &nopReaderImpl{b: b} }
func (r *nopReaderImpl) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

// writeTempPDF creates a fake PDF file and returns its path.
func writeTempPDF(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.pdf")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- Happy path ---

func TestImport_recognizeSucceeds_returnsTitleAndItemType(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{
		pollFn: func(ctx context.Context, sid string) (*RecognizedResp, error) {
			return &RecognizedResp{
				Recognized: true,
				Title:      "Attention Is All You Need",
				ItemType:   "journalArticle",
			}, nil
		},
	}
	path := writeTempPDF(t, "%PDF-1.4 contents")
	res, err := Import(context.Background(), ft, path, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !res.Recognized || res.Title != "Attention Is All You Need" || res.ItemType != "journalArticle" {
		t.Errorf("result = %+v", res)
	}
	if ft.pingCalls != 1 || ft.saveCalls != 1 || ft.pollCalls != 1 {
		t.Errorf("calls: ping=%d save=%d recognize=%d; want 1/1/1", ft.pingCalls, ft.saveCalls, ft.pollCalls)
	}
	if string(ft.capturedBytes) != "%PDF-1.4 contents" {
		t.Errorf("bytes read from disk do not match: got %q", ft.capturedBytes)
	}
	if ft.capturedMeta.Title != "fixture.pdf" {
		t.Errorf("meta.Title = %q, want fixture.pdf", ft.capturedMeta.Title)
	}
	if ft.capturedMeta.SessionID == "" {
		t.Error("meta.SessionID must be non-empty")
	}
}

// --- Sad paths ---

func TestImport_canRecognizeFalse_shortCircuitsWithoutRecognize(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{
		saveFn: func(ctx context.Context, body io.Reader, meta SaveMeta) (*SaveResp, error) {
			return &SaveResp{CanRecognize: false}, nil
		},
	}
	path := writeTempPDF(t, "x")
	res, err := Import(context.Background(), ft, path, Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Recognized {
		t.Error("result.Recognized must be false when desktop declines to recognize")
	}
	if ft.pollCalls != 0 {
		t.Errorf("GetRecognizedItem calls = %d, want 0 when canRecognize=false", ft.pollCalls)
	}
	if res.Message == "" {
		t.Error("result.Message should explain the canRecognize=false outcome")
	}
}

func TestImport_recognize204_surfacedAsNotRecognized(t *testing.T) {
	t.Parallel()
	// Desktop returns Recognized=false (204 on the wire) when recognition
	// completes but can't identify the PDF. Distinct from timeout.
	ft := &fakeTransport{
		pollFn: func(ctx context.Context, sid string) (*RecognizedResp, error) {
			return &RecognizedResp{Recognized: false}, nil
		},
	}
	path := writeTempPDF(t, "x")
	res, err := Import(context.Background(), ft, path, Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Recognized {
		t.Error("Recognized must stay false on 204")
	}
	if res.Message == "" || !strings.Contains(res.Message, "couldn't identify") {
		t.Errorf("Message should describe the no-match outcome, got %q", res.Message)
	}
}

func TestImport_timeoutReturnsPartialResult(t *testing.T) {
	t.Parallel()
	// When ctx hits its deadline during getRecognizedItem, Import must
	// return a partial result (err==nil, Recognized=false) with a message
	// telling the user to check desktop.
	ft := &fakeTransport{
		pollFn: func(ctx context.Context, sid string) (*RecognizedResp, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	path := writeTempPDF(t, "x")
	res, err := Import(context.Background(), ft, path, Options{Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("Import must not error on timeout — partial result expected: %v", err)
	}
	if res.Recognized {
		t.Error("Recognized must be false after timeout")
	}
	if res.Message == "" || !strings.Contains(res.Message, "did not complete") {
		t.Errorf("Message should describe the timeout, got %q", res.Message)
	}
}

func TestImport_desktopUnreachable_errors(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{pingErr: ErrDesktopUnreachable}
	path := writeTempPDF(t, "x")
	_, err := Import(context.Background(), ft, path, Options{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected error when Ping reports desktop unreachable")
	}
	if !errors.Is(err, ErrDesktopUnreachable) {
		t.Errorf("err = %v, want wrapping ErrDesktopUnreachable", err)
	}
	if ft.saveCalls != 0 {
		t.Error("must not attempt upload when desktop is unreachable")
	}
}

func TestImport_fileNotFound_errors(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{}
	_, err := Import(context.Background(), ft, "/no/such/path.pdf", Options{Timeout: time.Second})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if ft.saveCalls != 0 {
		t.Error("must not call Save when the file cannot be opened")
	}
}

func TestImport_noWaitSkipsRecognize(t *testing.T) {
	t.Parallel()
	ft := &fakeTransport{}
	path := writeTempPDF(t, "x")
	res, err := Import(context.Background(), ft, path, Options{NoWait: true, Timeout: time.Second})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if ft.pollCalls != 0 {
		t.Errorf("GetRecognizedItem calls = %d, want 0 under --no-wait", ft.pollCalls)
	}
	if res.Recognized {
		t.Error("no-wait cannot claim recognition")
	}
	if res.Message == "" {
		t.Error("Message should explain that recognition was not awaited")
	}
}

func TestImport_contextCancelAbortsRecognize(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	ft := &fakeTransport{
		pollFn: func(ctx context.Context, _ string) (*RecognizedResp, error) {
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	path := writeTempPDF(t, "x")
	_, err := Import(ctx, ft, path, Options{Timeout: time.Hour})
	if err == nil {
		t.Fatal("expected error when parent ctx is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
