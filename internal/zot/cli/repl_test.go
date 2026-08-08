package cli

// Tests for the `zot browse` REPL core. The loop is driven through
// scripted readLine input against the shared orient fixture; the
// system-viewer launch, file stat, and content-index widener are all
// injected so no test spawns `open` or touches the real user cache.

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
)

// seedREPLAttachments adds PDF attachments to the orient fixture: one on
// the personal item KEY1 and one on the group item GRPKEY01. KEY2 stays
// attachment-free on purpose (the no-PDF path).
func seedREPLAttachments(t *testing.T, dataDir string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "zotero.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	stmts := []string{
		`INSERT INTO items (itemID, itemTypeID, libraryID, key, version, dateAdded, dateModified, clientDateModified) VALUES
			(100, 1, 1, 'ATTKEY01', 1, '2024-01-01 10:05:00', '2024-01-01 10:05:00', '2024-01-01 10:05:00'),
			(101, 1, 2, 'ATTKEY02', 1, '2024-06-01 10:05:00', '2024-06-01 10:05:00', '2024-06-01 10:05:00')`,
		`INSERT INTO itemAttachments (itemID, parentItemID, linkMode, contentType, path) VALUES
			(100, 1, 1, 'application/pdf', 'storage:paper1.pdf'),
			(101, 7, 1, 'application/pdf', 'storage:shared1.pdf')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

// scriptedLines returns a readLine that replays lines then reports EOF —
// the same shape Ctrl-D produces in production.
func scriptedLines(lines ...string) func() (string, error) {
	i := 0
	return func() (string, error) {
		if i >= len(lines) {
			return "", io.EOF
		}
		line := lines[i]
		i++
		return line, nil
	}
}

// replHarness bundles a browseREPL wired against the orient fixture with
// recorders for every injected side effect.
type replHarness struct {
	repl     *browseREPL
	ctx      context.Context
	out      *strings.Builder
	launched []string
}

// newREPLHarness opens the fixture at the given scope and returns a
// harness whose launch recorder appends instead of spawning `open`.
func newREPLHarness(t *testing.T, scope zot.LibraryScope, lines ...string) *replHarness {
	t.Helper()
	dataDir := withSharedOrientConfig(t)
	seedREPLAttachments(t, dataDir)

	cfg, err := zot.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ref, err := cfg.Resolve(scope)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", scope, err)
	}
	sel, err := localSelectorFor(cfg, ref)
	if err != nil {
		t.Fatalf("selector: %v", err)
	}
	db, err := local.Open(cfg.DataDir, sel)
	if err != nil {
		t.Fatalf("open local: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	h := &replHarness{out: &strings.Builder{}}
	h.repl = &browseREPL{
		cfg:      cfg,
		ref:      ref,
		db:       db,
		limit:    15,
		out:      h.out,
		readLine: scriptedLines(lines...),
		launch: func(path string) error {
			h.launched = append(h.launched, path)
			return nil
		},
		stat:    func(string) error { return nil },
		widener: contentWidener,
	}
	h.ctx = withLibraryHolder(context.Background(), &libraryHolder{
		HasFlag: true, Partial: scope, Resolved: &ref,
	})
	return h
}

func (h *replHarness) run(t *testing.T) {
	t.Helper()
	if err := h.repl.run(h.ctx); err != nil {
		t.Fatalf("repl run: %v\noutput:\n%s", err, h.out.String())
	}
}

func TestBrowseREPL_SearchThenOpen(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, "Paper", "1", ":q")
	h.run(t)

	out := h.out.String()
	if !strings.Contains(out, "Paper One") {
		t.Fatalf("results missing 'Paper One':\n%s", out)
	}
	if !strings.Contains(out, "1.") {
		t.Errorf("results not numbered:\n%s", out)
	}
	// KEY1 is the only dated hit, so title-tie ranking puts it first;
	// opening "1" must resolve its PDF under <dataDir>/storage/.
	if len(h.launched) != 1 {
		t.Fatalf("launched = %v, want exactly one open", h.launched)
	}
	want := filepath.Join(h.repl.cfg.DataDir, "storage", "ATTKEY01", "paper1.pdf")
	if h.launched[0] != want {
		t.Errorf("launched %q, want %q", h.launched[0], want)
	}
	if !strings.Contains(out, "opened") {
		t.Errorf("no confirmation line:\n%s", out)
	}
}

func TestBrowseREPL_OpenGroupItemUnderAll(t *testing.T) {
	// "Cortex" matches only the group item GRPKEY01 — resolution must
	// span the merged scope (the ResolvePDFAttachment libIn conversion).
	h := newREPLHarness(t, zot.LibAll, "Cortex", "1", ":q")
	h.run(t)

	want := filepath.Join(h.repl.cfg.DataDir, "storage", "ATTKEY02", "shared1.pdf")
	if len(h.launched) != 1 || h.launched[0] != want {
		t.Fatalf("launched = %v, want [%s]\noutput:\n%s", h.launched, want, h.out.String())
	}
}

func TestBrowseREPL_OpenBeforeSearch(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, "1", ":q")
	h.run(t)

	if len(h.launched) != 0 {
		t.Fatalf("launched %v before any search", h.launched)
	}
	if !strings.Contains(h.out.String(), "search first") {
		t.Errorf("expected guidance to search first:\n%s", h.out.String())
	}
}

func TestBrowseREPL_OpenOutOfRange(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, "Paper", "99", ":q")
	h.run(t)

	if len(h.launched) != 0 {
		t.Fatalf("launched %v for out-of-range pick", h.launched)
	}
	if !strings.Contains(h.out.String(), "99") {
		t.Errorf("message should echo the bad number:\n%s", h.out.String())
	}
}

func TestBrowseREPL_NoPDF(t *testing.T) {
	// "Two" matches only KEY2, which has no attachment.
	h := newREPLHarness(t, zot.LibAll, "Two", "1", ":q")
	h.run(t)

	if len(h.launched) != 0 {
		t.Fatalf("launched %v for item without PDF", h.launched)
	}
	if !strings.Contains(h.out.String(), "no PDF") {
		t.Errorf("expected a no-PDF message:\n%s", h.out.String())
	}
}

func TestBrowseREPL_StatMissing(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, "Paper", "1", ":q")
	h.repl.stat = func(string) error { return errors.New("gone") }
	h.run(t)

	if len(h.launched) != 0 {
		t.Fatalf("launched %v despite missing file", h.launched)
	}
	if !strings.Contains(h.out.String(), "paper1.pdf") {
		t.Errorf("message should name the missing path:\n%s", h.out.String())
	}
}

func TestBrowseREPL_LimitCommand(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":limit 2", "Paper", ":q")
	h.run(t)

	out := h.out.String()
	if !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Fatalf("expected two rows:\n%s", out)
	}
	if strings.Contains(out, "3.") {
		t.Errorf(":limit 2 leaked a third row:\n%s", out)
	}
}

func TestBrowseREPL_LimitBadArg(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":limit x", ":q")
	h.run(t)

	if !strings.Contains(h.out.String(), ":limit") {
		t.Errorf("expected usage guidance for :limit:\n%s", h.out.String())
	}
}

func TestBrowseREPL_ContentRefusedUnderAll(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":content", ":q")
	h.run(t)

	if h.repl.csearch != nil {
		t.Fatal("content toggled on under --library all; per-library index makes that a silently narrower answer")
	}
	if !strings.Contains(h.out.String(), ":library") {
		t.Errorf("refusal should point at :library:\n%s", h.out.String())
	}
}

func TestBrowseREPL_ContentMissingIndex(t *testing.T) {
	h := newREPLHarness(t, zot.LibPersonal, ":content", ":q")
	// Stub the widener with the same coded error the real one returns
	// for an unbuilt index — the REPL must print the fix and continue.
	h.repl.widener = func(context.Context, local.Reader) (*contentSearch, error) {
		return nil, cmdutil.Coded(cmdutil.CodeNotFound, "no content index for this library").
			WithFix("sci zot content build --library personal")
	}
	h.run(t)

	if h.repl.csearch != nil {
		t.Fatal("content should stay off when the index is missing")
	}
	if !strings.Contains(h.out.String(), "content build") {
		t.Errorf("expected the build fix to surface:\n%s", h.out.String())
	}
}

func TestBrowseREPL_ContentToggleOff(t *testing.T) {
	h := newREPLHarness(t, zot.LibPersonal, ":content", ":q")
	closed := false
	h.repl.csearch = &contentSearch{close: func() { closed = true }}
	h.run(t)

	if h.repl.csearch != nil {
		t.Fatal("second :content should toggle off")
	}
	if !closed {
		t.Error("toggling off must close the index handle")
	}
}

func TestBrowseREPL_LibrarySwitchNarrowsPool(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":library personal", "Paper", ":q")
	h.run(t)

	out := h.out.String()
	if strings.Contains(out, "Cortex") {
		t.Fatalf("group item leaked into personal scope:\n%s", out)
	}
	if h.repl.ref.Scope != zot.LibPersonal {
		t.Errorf("ref.Scope = %s, want personal", h.repl.ref.Scope)
	}
}

func TestBrowseREPL_LibraryAllDisablesContent(t *testing.T) {
	h := newREPLHarness(t, zot.LibPersonal, ":library all", ":q")
	closed := false
	h.repl.csearch = &contentSearch{close: func() { closed = true }}
	h.run(t)

	if h.repl.csearch != nil {
		t.Fatal("content must auto-disable when scope widens to all")
	}
	if !closed {
		t.Error("the old index handle must be closed on scope switch")
	}
	if !strings.Contains(h.out.String(), "content") {
		t.Errorf("auto-disable should be announced:\n%s", h.out.String())
	}
}

func TestBrowseREPL_LibraryBogus(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":library bogus", ":q")
	h.run(t)

	out := h.out.String()
	if !strings.Contains(out, "personal") || !strings.Contains(out, "shared") {
		t.Errorf("bad scope message should list valid values:\n%s", out)
	}
	if h.repl.ref.Scope != zot.LibAll {
		t.Errorf("scope changed on bogus input: %s", h.repl.ref.Scope)
	}
}

func TestBrowseREPL_ProvenanceMarkers(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, "Cortex", ":q")
	h.run(t)
	if !strings.Contains(h.out.String(), "[shared]") {
		t.Errorf("merged scope should mark shared rows:\n%s", h.out.String())
	}

	h2 := newREPLHarness(t, zot.LibPersonal, "Paper", ":q")
	h2.run(t)
	if strings.Contains(h2.out.String(), "[personal]") || strings.Contains(h2.out.String(), "[shared]") {
		t.Errorf("single-library scope should not mark rows:\n%s", h2.out.String())
	}
}

func TestBrowseREPL_UnknownCommandAndHelp(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll, ":wat", ":h", "", ":q")
	h.run(t)

	out := h.out.String()
	if !strings.Contains(out, ":h") {
		t.Errorf("unknown command should point at :h:\n%s", out)
	}
	if !strings.Contains(out, ":content") {
		t.Errorf("help should list :content:\n%s", out)
	}
}

func TestBrowseREPL_EOFExitsCleanly(t *testing.T) {
	h := newREPLHarness(t, zot.LibAll) // immediate EOF, like Ctrl-D
	h.run(t)
}

func TestBrowseCommand_JSONRejected(t *testing.T) {
	withSharedOrientConfig(t)

	_, err := runOrient(t, "--json", "--library", "personal", "browse")
	if err == nil {
		t.Fatal("browse under --json must error — the REPL has no non-interactive mode")
	}
	if !strings.Contains(err.Error(), "interactive") {
		t.Errorf("error should teach why: %v", err)
	}
}

func TestBrowseCommand_NonTTYRejected(t *testing.T) {
	withSharedOrientConfig(t)

	// Under `go test` stdin is not a terminal, so the guard trips
	// without any stubbing — exactly the agent/pipe/CI case.
	_, err := runOrient(t, "--library", "personal", "browse")
	if err == nil {
		t.Fatal("browse without a TTY must error, not hang waiting for input")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should name the missing terminal: %v", err)
	}
}

func TestBrowseScope_Defaulting(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		hasFlag          bool
		partial          zot.LibraryScope
		sharedConfigured bool
		want             zot.LibraryScope
	}{
		{"explicit flag wins", true, zot.LibShared, true, zot.LibShared},
		{"no flag, shared configured → all", false, "", true, zot.LibAll},
		{"no flag, personal only → personal", false, "", false, zot.LibPersonal},
		{"explicit personal beats default all", true, zot.LibPersonal, true, zot.LibPersonal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := browseScope(tc.hasFlag, tc.partial, tc.sharedConfigured); got != tc.want {
				t.Errorf("browseScope(%v, %q, %v) = %s, want %s",
					tc.hasFlag, tc.partial, tc.sharedConfigured, got, tc.want)
			}
		})
	}
}
