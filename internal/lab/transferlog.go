package lab

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// TransferEntry records one rsync invocation. We append a "started" entry
// when a transfer begins and a "done" entry (sentinel: DoneAt set) when it
// finishes cleanly. PendingTransfers collapses the log latest-wins per
// Remote and returns those still in flight.
type TransferEntry struct {
	Remote        string     `json:"remote"`
	Local         string     `json:"local"`
	ExpectedBytes int64      `json:"size,omitempty"`
	StartedAt     time.Time  `json:"started"`
	DoneAt        *time.Time `json:"done,omitempty"`
}

// TransferLogPath returns the manifest path. Honors SCI_LAB_TRANSFER_LOG;
// defaults to ~/.config/sci/lab-transfers.jsonl (next to lab.json).
func TransferLogPath() string {
	if p := os.Getenv("SCI_LAB_TRANSFER_LOG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sci", "lab-transfers.jsonl")
}

// LogTransferStarted appends a "started" entry for e. StartedAt is set to
// time.Now() if zero. Creates the parent directory if missing.
func LogTransferStarted(e TransferEntry) error {
	if e.StartedAt.IsZero() {
		e.StartedAt = time.Now()
	}
	e.DoneAt = nil
	return appendEntry(e)
}

// LogTransferDone appends a sentinel "done" entry for remote. Subsequent
// calls to PendingTransfers will exclude this remote.
func LogTransferDone(remote string) error {
	now := time.Now()
	return appendEntry(TransferEntry{Remote: remote, DoneAt: &now})
}

// PendingTransfers returns transfers that started but haven't completed and
// whose local destination still looks unfinished (missing, or smaller than
// ExpectedBytes). Returns an empty slice if the log file doesn't exist.
// Malformed lines are silently skipped so a hand-edited log doesn't crash.
func PendingTransfers() ([]TransferEntry, error) {
	f, err := os.Open(TransferLogPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Latest-wins per Remote. A "done" entry erases the started entry.
	latest := map[string]TransferEntry{}
	order := []string{} // insertion order for stable output
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e TransferEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.Remote == "" {
			continue
		}
		if _, seen := latest[e.Remote]; !seen {
			order = append(order, e.Remote)
		}
		latest[e.Remote] = e
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	pending := make([]TransferEntry, 0, len(order))
	for _, k := range order {
		e := latest[k]
		if e.DoneAt != nil {
			continue
		}
		if !looksUnfinished(e) {
			continue
		}
		pending = append(pending, e)
	}
	return pending, nil
}

// looksUnfinished returns true if e's local destination still needs more
// bytes — i.e. the file is missing or smaller than ExpectedBytes (or
// ExpectedBytes is unknown, in which case we trust the manifest).
//
// For directory transfers (rsync of a remote dir) the destination on disk is
// itself a directory; comparing its inode size against ExpectedBytes is
// meaningless. We sum sizes of all regular files inside instead so a
// completed dir transfer is correctly recognised as finished.
func looksUnfinished(e TransferEntry) bool {
	if e.Local == "" {
		return true
	}
	st, err := os.Stat(e.Local)
	if err != nil {
		// Local missing → user wiped it; nothing to resume.
		return false
	}
	have := st.Size()
	if st.IsDir() {
		have = localDirSize(e.Local)
	}
	if e.ExpectedBytes > 0 && have >= e.ExpectedBytes {
		return false
	}
	return true
}

// localDirSize sums sizes of all regular files under root. Used to compare a
// directory transfer's on-disk progress against its ExpectedBytes total. Walk
// errors are swallowed: a partial size is better than declaring the transfer
// finished when it isn't, and resume is always safe (rsync recomputes deltas).
func localDirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// ClearTransferLog removes the manifest entirely, so PendingTransfers will
// return an empty slice on the next call. A missing file is treated as
// already-cleared (no error). Used by the browse TUI's "c" keybind to drop
// all resumable downloads when the user no longer wants them.
func ClearTransferLog() error {
	if err := os.Remove(TransferLogPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear transfer log: %w", err)
	}
	return nil
}

func appendEntry(e TransferEntry) error {
	path := TransferLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create transfer log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open transfer log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := json.NewEncoder(f).Encode(e); err != nil {
		return fmt.Errorf("write transfer log: %w", err)
	}
	return nil
}
