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

// stderrSentinelTable is the (non-existent) table name we reference from a
// synthetic statement appended to every query. The resulting catalog-error
// line on stderr is our synchronisation point: query() can only sample
// stderrBuf after drainStderr has observed this marker, so user-statement
// errors that race the stdout sentinel are guaranteed visible.
const stderrSentinelTable = "__sci_duck_stderr_sentinel__"

// stderrDrainTimeout bounds query()'s wait for drainStderr to observe the
// per-query stderr sentinel after the stdout sentinel arrives. The drain
// is normally signalled within microseconds; the timeout is purely defensive
// and only fires if duckdb's stderr stream stalls.
const stderrDrainTimeout = 2 * time.Second

// stderrSyntheticTrailerLines is how many stderr lines follow the line that
// matches our synthetic-sentinel marker before the catalog-error block
// finishes. duckdb's "Catalog Error: Table X does not exist!" is followed
// by exactly 4 more lines: a "Did you mean ..." hint, a blank separator,
// a "LINE 1: ..." echo of the offending statement, and a caret-pointer
// line. drainStderr drops these regardless of arm state — without that,
// lines that haven't reached the drain by the time the next query()
// re-arms get appended to the new query's stderrBuf and surface as
// spurious errors (a lone "^" from the caret line was the classic
// symptom). See TestDrainStderr_TrailerDoesNotLeakAcrossQueries.
const stderrSyntheticTrailerLines = 4

// subproc owns one duckdb child process. Methods are safe to call from
// any goroutine but serialise through [subproc.mu] — duckdb's stdin is
// a single command stream.
type subproc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu      sync.Mutex // serialises query()
	counter atomic.Uint64

	// stderrMu guards stderrBuf and the sentinel-arming fields below. The
	// drain goroutine and query() both touch them.
	stderrMu  sync.Mutex
	stderrBuf strings.Builder
	stderrDie chan struct{} // closed when the stderr drain goroutine exits

	// stderrSentinel, when non-empty, is the per-query marker drainStderr
	// is watching for. On match the goroutine closes stderrSeen, clears
	// both fields, and arms stderrSyntheticRemaining so the trailing 4
	// lines of the catalog error are dropped even if the next query has
	// already re-armed by the time they arrive.
	stderrSentinel string
	stderrSeen     chan struct{}

	// stderrSyntheticRemaining counts the trailing synthetic-error lines
	// drainStderr still owes itself a drop on. It persists across query()
	// arms so the trailer cannot leak into the next query's stderrBuf.
	stderrSyntheticRemaining int

	closed atomic.Bool

	// closeTimeout overrides defaultCloseTimeout per-instance. Tests set this
	// to a short duration to drive the SIGKILL fallback without making the
	// suite slow. Set in startSubprocFromCmd, immutable thereafter.
	closeTimeout time.Duration
}

// startSubproc spawns `duckdb -jsonlines <dbPath>` in read-write mode.
// Returns [duckcli.ErrNotInstalled] when the binary is missing.
func startSubproc(dbPath string) (*subproc, error) {
	// Reject paths beginning with `-` — duckdb has no `--` separator, so a
	// path like `-c "ATTACH '/etc/passwd'"` would be parsed as a flag.
	// Defense in depth; callers typically pass paths they own, but a foreign
	// .duckdb file dropped into the cwd shouldn't be openable as `sci view -x.duckdb`.
	if strings.HasPrefix(dbPath, "-") {
		return nil, fmt.Errorf("refusing duckdb path starting with %q: %s", "-", dbPath)
	}
	if !duckcli.Available() {
		return nil, duckcli.ErrNotInstalled
	}
	cmd := exec.Command("duckdb", "-jsonlines", dbPath) //nolint:gosec // binary name is fixed, path validated above
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

// drainStderr is the sole reader of the child's stderr stream. It operates
// as a small state machine, guarded by stderrMu:
//
//   - draining trailer (stderrSyntheticRemaining > 0): silently drop the
//     line and decrement. Set after a sentinel match; persists across the
//     next query()'s arm so the catalog-error trailer (hint, blank, LINE 1:
//     echo, caret) never leaks into the wrong query's buf.
//   - armed (stderrSentinel != ""): a query is in flight. Append each line
//     to stderrBuf until one contains the sentinel marker; on match close
//     stderrSeen, arm stderrSyntheticRemaining, and disarm. This is the
//     synchronisation point query() waits on before sampling stderrBuf.
//   - disarmed (stderrSentinel == "" and no trailer pending): no query is
//     waiting. Discard the line.
func (s *subproc) drainStderr(r io.Reader) {
	defer close(s.stderrDie)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		s.stderrMu.Lock()
		if s.stderrSyntheticRemaining > 0 {
			s.stderrSyntheticRemaining--
			s.stderrMu.Unlock()
			continue
		}
		if s.stderrSentinel == "" {
			s.stderrMu.Unlock()
			continue
		}
		if strings.Contains(line, s.stderrSentinel) {
			if s.stderrSeen != nil {
				close(s.stderrSeen)
				s.stderrSeen = nil
			}
			s.stderrSentinel = ""
			s.stderrSyntheticRemaining = stderrSyntheticTrailerLines
			s.stderrMu.Unlock()
			continue
		}
		s.stderrBuf.WriteString(line)
		s.stderrBuf.WriteByte('\n')
		s.stderrMu.Unlock()
	}
}

// query sends sql followed by two framing statements — a synthetic SELECT
// against a non-existent table whose name embeds a per-query marker, then
// the stdout sentinel SELECT — and reads stdout lines until the sentinel
// arrives. A non-nil error is returned when duckdb wrote anything to stderr
// while the user statement was processed; the rows slice may still be
// non-empty if the failing statement was after a successful one (rare for
// single-query callers).
//
// The two-sentinel framing exists to synchronise the two independent
// readers — the goroutine main loop here reads stdout, while drainStderr
// reads stderr — so a failing user statement's stderr error is always
// observed *before* we sample stderrBuf. Without this, a fast stdout
// sentinel could race ahead of a slow stderr drain and we'd return
// (rows=nil, err=nil) for a statement duckdb actually rejected.
func (s *subproc) query(sql string) ([][]byte, error) {
	if s.closed.Load() {
		return nil, errors.New("duckdb subprocess closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := s.counter.Add(1)
	marker := fmt.Sprintf("end_%d", n)
	stderrMarker := fmt.Sprintf("%s%d__", stderrSentinelTable, n)

	// Arm drainStderr: it will append user-error lines to stderrBuf until it
	// sees stderrMarker, then signal stderrSeen and ignore the trailing
	// lines of the synthetic catalog error.
	seen := make(chan struct{})
	s.stderrMu.Lock()
	s.stderrBuf.Reset()
	s.stderrSentinel = stderrMarker
	s.stderrSeen = seen
	s.stderrMu.Unlock()
	defer s.disarmStderrSentinel()

	// Statements are terminated explicitly; the sentinel is a tiny SELECT
	// that always succeeds and renders as `{"__sci_duck_sentinel__":"end_N"}`.
	// We strip any trailing `;` or whitespace from the caller's SQL so the
	// statement separator we append is the only one in play — otherwise
	// duckdb sees one combined statement and parse-errors.
	user := strings.TrimRight(sql, "; \t\r\n")
	payload := fmt.Sprintf(
		"%s;\nSELECT 1 FROM %s;\nSELECT '%s' AS %s;\n",
		user, stderrMarker, marker, sentinelKey,
	)
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

	// Wait for drainStderr to observe the stderr marker. Without this,
	// stderrSnapshot() can return "" for a failing user statement whose
	// error line is still buffered in the OS pipe.
	select {
	case <-seen:
	case <-s.stderrDie:
		// Drain goroutine exited (subprocess died). Snapshot whatever we have.
	case <-time.After(stderrDrainTimeout):
		// Defensive: stderr stream stalled. Snapshot anyway.
	}

	if errText := s.stderrSnapshot(); errText != "" {
		return rows, fmt.Errorf("duckdb: %s", errText)
	}
	return rows, nil
}

// disarmStderrSentinel is called via defer from query() to make sure the
// drain goroutine isn't left armed if we return early (stdin write failure,
// stdout EOF). Idempotent: a no-op when drainStderr already consumed the
// sentinel.
func (s *subproc) disarmStderrSentinel() {
	s.stderrMu.Lock()
	s.stderrSentinel = ""
	s.stderrSeen = nil
	s.stderrMu.Unlock()
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
//
// duckdb sets a non-zero exit status when any statement in the session
// failed. Our per-query stderr sentinel (see [subproc.query]) deliberately
// fails, so every well-behaved session exits 1 — that's not a real error.
// We surface signal-induced terminations (SIGKILL fallback below, segfaults,
// etc.) and ignore polite non-zero exits.
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
		return filterPoliteExit(err)
	case <-time.After(s.closeTimeout):
		// SIGKILL the whole group — a stuck duckdb may have ignored .exit,
		// and any descendants reparented to init would otherwise leak.
		if s.cmd.Process != nil {
			_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		}
		return <-waitErr
	}
}

// filterPoliteExit suppresses the *exec.ExitError that duckdb returns on a
// clean shutdown after the synthetic stderr sentinel raised an error during
// the session. Signal-induced terminations are still propagated.
func filterPoliteExit(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return err
	}
	return nil
}
