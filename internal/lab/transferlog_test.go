package lab

import (
	"os"
	"path/filepath"
	"testing"
)

func setLogTo(t *testing.T, path string) {
	t.Helper()
	t.Setenv("SCI_LAB_TRANSFER_LOG", path)
}

func TestTransferLog_StartAppendsEntry(t *testing.T) {
	setLogTo(t, filepath.Join(t.TempDir(), "log.jsonl"))
	if err := LogTransferStarted(TransferEntry{Remote: "/labs/sciminds/a", Local: "./a", ExpectedBytes: 100}); err != nil {
		t.Fatalf("LogTransferStarted: %v", err)
	}
	data, err := os.ReadFile(TransferLogPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := `"remote":"/labs/sciminds/a"`; !contains(string(data), want) {
		t.Errorf("expected log to contain %q; got:\n%s", want, data)
	}
}

func TestTransferLog_DoneRemovesFromPending(t *testing.T) {
	dir := t.TempDir()
	setLogTo(t, filepath.Join(dir, "log.jsonl"))
	// Local files exist + are smaller than ExpectedBytes so they'd otherwise be pending.
	mustTouch(t, filepath.Join(dir, "a"))
	mustTouch(t, filepath.Join(dir, "b"))
	_ = LogTransferStarted(TransferEntry{Remote: "/r/a", Local: filepath.Join(dir, "a"), ExpectedBytes: 100})
	_ = LogTransferStarted(TransferEntry{Remote: "/r/b", Local: filepath.Join(dir, "b"), ExpectedBytes: 100})
	if err := LogTransferDone("/r/a"); err != nil {
		t.Fatalf("LogTransferDone: %v", err)
	}
	pending, err := PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 1 || pending[0].Remote != "/r/b" {
		t.Errorf("Pending = %+v, want only /r/b", pending)
	}
}

func TestTransferLog_PendingDropsMissingLocal(t *testing.T) {
	dir := t.TempDir()
	setLogTo(t, filepath.Join(dir, "log.jsonl"))
	_ = LogTransferStarted(TransferEntry{Remote: "/r/x", Local: filepath.Join(dir, "nope"), ExpectedBytes: 100})
	pending, _ := PendingTransfers()
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty (local missing)", pending)
	}
}

func TestTransferLog_PendingDropsCompletedSize(t *testing.T) {
	dir := t.TempDir()
	setLogTo(t, filepath.Join(dir, "log.jsonl"))
	full := filepath.Join(dir, "full")
	if err := os.WriteFile(full, make([]byte, 100), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = LogTransferStarted(TransferEntry{Remote: "/r/full", Local: full, ExpectedBytes: 100})
	pending, _ := PendingTransfers()
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty (size matches)", pending)
	}
}

func TestTransferLog_PendingKeepsShortLocal(t *testing.T) {
	dir := t.TempDir()
	setLogTo(t, filepath.Join(dir, "log.jsonl"))
	short := filepath.Join(dir, "short")
	if err := os.WriteFile(short, make([]byte, 10), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = LogTransferStarted(TransferEntry{Remote: "/r/short", Local: short, ExpectedBytes: 100})
	pending, _ := PendingTransfers()
	if len(pending) != 1 {
		t.Errorf("Pending = %+v, want 1 (size short)", pending)
	}
}

func TestTransferLog_PendingSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log.jsonl")
	setLogTo(t, logPath)
	mustTouch(t, filepath.Join(dir, "a"))
	if err := os.WriteFile(logPath, []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = LogTransferStarted(TransferEntry{Remote: "/r/a", Local: filepath.Join(dir, "a"), ExpectedBytes: 100})
	pending, err := PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 1 || pending[0].Remote != "/r/a" {
		t.Errorf("Pending = %+v, want only /r/a (malformed line ignored)", pending)
	}
}

func TestTransferLog_StartCreatesParentDirs(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "log.jsonl")
	setLogTo(t, deep)
	if err := LogTransferStarted(TransferEntry{Remote: "/r/x", Local: "/tmp/x", ExpectedBytes: 1}); err != nil {
		t.Fatalf("LogTransferStarted: %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Errorf("expected log file created: %v", err)
	}
}

func TestTransferLog_ClearRemovesPending(t *testing.T) {
	dir := t.TempDir()
	setLogTo(t, filepath.Join(dir, "log.jsonl"))
	short := filepath.Join(dir, "short")
	if err := os.WriteFile(short, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = LogTransferStarted(TransferEntry{Remote: "/r/x", Local: short, ExpectedBytes: 100})
	if err := ClearTransferLog(); err != nil {
		t.Fatalf("ClearTransferLog: %v", err)
	}
	pending, err := PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty after clear", pending)
	}
}

func TestTransferLog_ClearMissingFileIsNoop(t *testing.T) {
	setLogTo(t, filepath.Join(t.TempDir(), "nope.jsonl"))
	if err := ClearTransferLog(); err != nil {
		t.Errorf("ClearTransferLog on missing file should be a no-op, got: %v", err)
	}
}

func TestTransferLog_PendingMissingFileIsEmpty(t *testing.T) {
	setLogTo(t, filepath.Join(t.TempDir(), "nonexistent.jsonl"))
	pending, err := PendingTransfers()
	if err != nil {
		t.Fatalf("PendingTransfers: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Pending = %+v, want empty (file does not exist)", pending)
	}
}

// helpers
func mustTouch(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte{}, 0o600); err != nil {
		t.Fatalf("touch %s: %v", p, err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
