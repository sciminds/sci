package duck

// subproc.go — long-running `duckdb -jsonlines <path>` wrapper. The
// single goroutine that drains stderr keeps us in sync with the
// process's view of errors; query/sentinel framing on stdin lets us
// associate each result run with the SQL that produced it.
//
// Phase 3 opens the subprocess read-write. Row-level mutations are
// still gated by the Store on PK availability; DDL/import mutations
// flow straight through. duckdb's own constraint checking surfaces any
// remaining violations on stderr.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	duckcli "github.com/sciminds/cli/internal/duck"
)

// sentinelKey is the column name we attach to the framing SELECT. It is
// long and non-ASCII-free to keep it from appearing inside legitimate
// row payloads.
const sentinelKey = "__sci_duck_sentinel__"

// subproc owns one duckdb child process. Methods are safe to call from
// any goroutine but serialise through [subproc.mu] — duckdb's stdin is
// a single command stream.
type subproc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex // serialises query()
	counter atomic.Uint64

	stderrMu  sync.Mutex
	stderrBuf strings.Builder
	stderrDie chan struct{} // closed when the stderr drain goroutine exits

	closed atomic.Bool
}

// startSubproc spawns `duckdb -jsonlines <dbPath>` in read-write mode.
// Returns [duckcli.ErrNotInstalled] when the binary is missing.
func startSubproc(dbPath string) (*subproc, error) {
	if !duckcli.Available() {
		return nil, duckcli.ErrNotInstalled
	}
	cmd := exec.Command("duckdb", "-jsonlines", dbPath) //nolint:gosec // binary name is fixed, path validated by caller
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start duckdb: %w", err)
	}

	s := &subproc{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewReaderSize(stdoutPipe, 1<<20), // 1 MB initial buffer; grows on demand
		stderrDie: make(chan struct{}),
	}
	go s.drainStderr(stderrPipe)
	return s, nil
}

// drainStderr appends every stderr line to the shared buffer so it can
// be sampled by query() right after each sentinel.
func (s *subproc) drainStderr(r io.Reader) {
	defer close(s.stderrDie)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		s.stderrMu.Lock()
		s.stderrBuf.WriteString(scanner.Text())
		s.stderrBuf.WriteByte('\n')
		s.stderrMu.Unlock()
	}
}

// query sends sql followed by a framing SELECT, reads stdout lines until
// the sentinel arrives, and returns the per-row JSON payloads. A non-nil
// error is returned when duckdb wrote anything to stderr while the
// statement was processed; the rows slice may still be non-empty if the
// failing statement was after a successful one (rare for single-query
// callers).
func (s *subproc) query(sql string) ([][]byte, error) {
	if s.closed.Load() {
		return nil, errors.New("duckdb subprocess closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear stderr from any earlier query.
	s.stderrMu.Lock()
	s.stderrBuf.Reset()
	s.stderrMu.Unlock()

	n := s.counter.Add(1)
	marker := fmt.Sprintf("end_%d", n)

	// Statements are terminated explicitly; the sentinel is a tiny SELECT
	// that always succeeds and renders as `{"__sci_duck_sentinel__":"end_N"}`.
	// We strip any trailing `;` or whitespace from the caller's SQL so the
	// statement separator we append is the only one in play — otherwise
	// duckdb sees one combined statement and parse-errors.
	user := strings.TrimRight(sql, "; \t\r\n")
	payload := fmt.Sprintf("%s;\nSELECT '%s' AS %s;\n", user, marker, sentinelKey)
	if _, err := io.WriteString(s.stdin, payload); err != nil {
		return nil, fmt.Errorf("write stdin: %w", err)
	}

	sentinelBytes := []byte(fmt.Sprintf(`"%s":"%s"`, sentinelKey, marker))
	var rows [][]byte
	for {
		line, err := s.stdout.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if bytes.Contains(line, sentinelBytes) {
				break
			}
			// Copy so callers cannot be invalidated by the bufio reuse.
			rows = append(rows, bytes.Clone(line))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("duckdb subprocess exited unexpectedly: %s", s.stderrSnapshot())
			}
			return nil, fmt.Errorf("read stdout: %w", err)
		}
	}

	if errText := s.stderrSnapshot(); errText != "" {
		return rows, fmt.Errorf("duckdb: %s", errText)
	}
	return rows, nil
}

// stderrSnapshot returns the current stderr buffer with leading/trailing
// whitespace trimmed. The buffer is not cleared — query() resets it
// before sending the next statement so a slow stderr drain still gets
// attributed to the right query.
func (s *subproc) stderrSnapshot() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	return strings.TrimSpace(s.stderrBuf.String())
}

// close shuts the subprocess down. Idempotent.
func (s *subproc) close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	// Best-effort polite shutdown: .exit cleanly closes the REPL.
	_, _ = io.WriteString(s.stdin, ".exit\n")
	_ = s.stdin.Close()
	<-s.stderrDie
	return s.cmd.Wait()
}
