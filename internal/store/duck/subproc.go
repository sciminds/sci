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
	"syscall"
	"time"

	duckcli "github.com/sciminds/cli/internal/duck"
)

// defaultCloseTimeout bounds how long [subproc.close] waits for the child
// to exit after .exit + stdin close before SIGKILLing the process group. A
// stuck query or a locked WAL otherwise hangs dbtui's shutdown.
//
// 3 s is generous: .exit should be near-instant; if duckdb hasn't returned
// by then it is not going to.
const defaultCloseTimeout = 3 * time.Second

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

	// closeTimeout overrides defaultCloseTimeout per-instance. Tests set this
	// to a short duration to drive the SIGKILL fallback without making the
	// suite slow. Set in startSubprocFromCmd, immutable thereafter.
	closeTimeout time.Duration
}

// startSubproc spawns `duckdb -jsonlines <dbPath>` in read-write mode.
// Returns [duckcli.ErrNotInstalled] when the binary is missing.
func startSubproc(dbPath string) (*subproc, error) {
	if !duckcli.Available() {
		return nil, duckcli.ErrNotInstalled
	}
	cmd := exec.Command("duckdb", "-jsonlines", dbPath) //nolint:gosec // binary name is fixed, path validated by caller
	return startSubprocFromCmd(cmd)
}

// startSubprocFromCmd is the shared entry point used by [startSubproc] and
// by tests that need to drive subproc semantics against a non-duckdb child
// (typically a hung `sleep` or a no-op `cat`) to exercise the close timeout
// without faking the duckdb binary.
//
// [Setpgid] puts the child in its own process group so [subproc.close] can
// SIGKILL the whole group as a fallback when .exit times out. Unix-only
// (darwin + linux); the project doesn't build on Windows.
func startSubprocFromCmd(cmd *exec.Cmd) (*subproc, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
		cmd:          cmd,
		stdin:        stdin,
		stdout:       bufio.NewReaderSize(stdoutPipe, 1<<20), // 1 MB initial buffer; grows on demand
		stderrDie:    make(chan struct{}),
		closeTimeout: defaultCloseTimeout,
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
			// duckdb -jsonlines emits a bare `\n` separator before
			// subsequent statements when the prior SELECT returned no
			// rows; ignore those blank lines so empty-result queries
			// don't surface a phantom row to the caller.
			if len(line) > 0 {
				// Copy so callers cannot be invalidated by the bufio reuse.
				rows = append(rows, bytes.Clone(line))
			}
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
//
// Polite path: write .exit, close stdin, wait for stderr drain, Wait().
// Hard path: if the child has not exited within [closeTimeout], SIGKILL the
// entire process group and continue waiting. A stuck query or a locked WAL
// would otherwise hang dbtui's shutdown indefinitely.
func (s *subproc) close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	_, _ = io.WriteString(s.stdin, ".exit\n")
	_ = s.stdin.Close()

	waitErr := make(chan error, 1)
	go func() {
		<-s.stderrDie
		waitErr <- s.cmd.Wait()
	}()
	select {
	case err := <-waitErr:
		return err
	case <-time.After(s.closeTimeout):
		// SIGKILL the whole group — a stuck duckdb may have ignored .exit,
		// and any descendants reparented to init would otherwise leak.
		if s.cmd.Process != nil {
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
		return <-waitErr
	}
}
