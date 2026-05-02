package cli

// Tests for ensureLibraryScope (auto-select / prompt / error / use-flag),
// the libraryHolder ctx plumbing, and outputScoped's library-injection
// behavior in JSON and human modes.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/urfave/cli/v3"
)

// stubResult is a minimal cmdutil.Result for outputScoped tests.
type stubResult struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

func (s stubResult) JSON() any     { return s }
func (s stubResult) Human() string { return "  ✓ " + s.Key + " " + s.Title + "\n" }

// stubResultWithLibrary is a Result that already has its own "library" key.
// Used to verify outputScoped doesn't inject a duplicate.
type stubResultWithLibrary struct {
	Library string `json:"library"`
	Key     string `json:"key"`
}

func (s stubResultWithLibrary) JSON() any     { return s }
func (s stubResultWithLibrary) Human() string { return s.Key }

// captureStdout swaps os.Stdout for a pipe and returns whatever was written
// up until restore() is called. Tests pair it with a defer.
func captureStdout(t *testing.T) (read func() string, restore func()) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		done <- buf
	}()
	read = func() string {
		_ = w.Close()
		return string(<-done)
	}
	restore = func() {
		os.Stdout = old
	}
	return read, restore
}

// ensureScopeCfg builds the minimal Config most ensureLibraryScope tests
// need. UserID is always set; SharedGroupID controls the "both libraries
// configured" branch.
func ensureScopeCfg(sharedGroupID string) *zot.Config {
	return &zot.Config{
		UserID:        "42",
		SharedGroupID: sharedGroupID,
	}
}

func TestEnsureLibraryScope_FlagSet_UsesIt(t *testing.T) {
	cfg := ensureScopeCfg("6506098")
	holder := &libraryHolder{HasFlag: true, Partial: zot.LibPersonal}
	ctx := withLibraryHolder(context.Background(), holder)

	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		t.Fatalf("ensureLibraryScope: %v", err)
	}
	if ref.Scope != zot.LibPersonal {
		t.Errorf("scope = %q, want personal", ref.Scope)
	}
	if holder.Resolved == nil || holder.Resolved.Scope != zot.LibPersonal {
		t.Errorf("holder not memoized: %+v", holder.Resolved)
	}
}

func TestEnsureLibraryScope_NoFlag_OnlyPersonal_AutoSelects(t *testing.T) {
	cfg := ensureScopeCfg("") // no shared
	holder := &libraryHolder{}
	ctx := withLibraryHolder(context.Background(), holder)

	// Capture stderr (auto-select log line) so it doesn't leak into test output.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	ref, err := ensureLibraryScope(ctx, cfg)
	_ = w.Close()
	stderr, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("ensureLibraryScope: %v", err)
	}
	if ref.Scope != zot.LibPersonal {
		t.Errorf("scope = %q, want auto personal", ref.Scope)
	}
	if !strings.Contains(string(stderr), "auto-selected personal") {
		t.Errorf("stderr = %q, want auto-select hint", string(stderr))
	}
}

func TestEnsureLibraryScope_NoFlag_BothConfigured_JSONMode_Errors(t *testing.T) {
	cfg := ensureScopeCfg("6506098")
	holder := &libraryHolder{JSONMode: true}
	ctx := withLibraryHolder(context.Background(), holder)

	_, err := ensureLibraryScope(ctx, cfg)
	if err == nil {
		t.Fatal("expected error in --json mode with both libraries configured")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "library") || !strings.Contains(msg, "json") {
		t.Errorf("err=%v, want mention of --library and --json", err)
	}
}

func TestEnsureLibraryScope_NoFlag_BothConfigured_Interactive_Prompts(t *testing.T) {
	cfg := ensureScopeCfg("6506098")
	holder := &libraryHolder{} // JSONMode=false (interactive)
	ctx := withLibraryHolder(context.Background(), holder)

	// Inject a deterministic prompter.
	var promptedWith []zot.LibraryScope
	prev := defaultLibraryPrompter
	defaultLibraryPrompter = func(opts []zot.LibraryScope) (zot.LibraryScope, error) {
		promptedWith = opts
		return zot.LibShared, nil
	}
	defer func() { defaultLibraryPrompter = prev }()

	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		t.Fatalf("ensureLibraryScope: %v", err)
	}
	if ref.Scope != zot.LibShared {
		t.Errorf("scope = %q, want shared (per stub picker)", ref.Scope)
	}
	wantOpts := []zot.LibraryScope{zot.LibPersonal, zot.LibShared}
	if len(promptedWith) != 2 || promptedWith[0] != wantOpts[0] || promptedWith[1] != wantOpts[1] {
		t.Errorf("prompter saw %v, want %v", promptedWith, wantOpts)
	}
}

func TestEnsureLibraryScope_NoFlag_BothConfigured_PromptCancel_Errors(t *testing.T) {
	cfg := ensureScopeCfg("6506098")
	holder := &libraryHolder{}
	ctx := withLibraryHolder(context.Background(), holder)

	prev := defaultLibraryPrompter
	defaultLibraryPrompter = func([]zot.LibraryScope) (zot.LibraryScope, error) {
		return "", errors.New("user aborted")
	}
	defer func() { defaultLibraryPrompter = prev }()

	if _, err := ensureLibraryScope(ctx, cfg); err == nil {
		t.Fatal("expected error when prompter cancels")
	}
}

func TestEnsureLibraryScope_Memoized(t *testing.T) {
	cfg := ensureScopeCfg("")
	holder := &libraryHolder{}
	ctx := withLibraryHolder(context.Background(), holder)

	// First call hits the auto-select log; suppress.
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	if _, err := ensureLibraryScope(ctx, cfg); err != nil {
		t.Fatalf("first call: %v", err)
	}
	_ = w.Close()
	os.Stderr = oldStderr

	// Second call should use memoized ref — no log emitted.
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()
	ref, err := ensureLibraryScope(ctx, cfg)
	_ = w.Close()
	stderr, _ := io.ReadAll(r)

	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if ref.Scope != zot.LibPersonal {
		t.Errorf("scope = %q, want personal (memoized)", ref.Scope)
	}
	if strings.Contains(string(stderr), "auto-selected") {
		t.Errorf("auto-select log fired twice; second call should be a cache hit")
	}
}

func TestEnsureLibraryScope_NoHolder_Errors(t *testing.T) {
	// Tests that bypass ResolveLibraryBefore (some unit-level harnesses
	// don't install one) get a clear error rather than silent personal.
	cfg := ensureScopeCfg("")
	if _, err := ensureLibraryScope(context.Background(), cfg); err == nil {
		t.Fatal("expected error when no holder is installed")
	}
}

func TestOutputScoped_JSON_InjectsLibrary(t *testing.T) {
	cmd := &cli.Command{Flags: []cli.Flag{cmdutil.JSONFlag(new(bool))}}
	if err := cmd.Set("json", "true"); err != nil {
		t.Fatalf("set json: %v", err)
	}

	holder := &libraryHolder{Resolved: &zot.LibraryRef{Scope: zot.LibPersonal, Name: "Personal"}}
	ctx := withLibraryHolder(context.Background(), holder)

	read, restore := captureStdout(t)
	defer restore()

	outputScoped(ctx, cmd, stubResult{Key: "ABC123", Title: "Hello"})
	got := read()

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %q", err, got)
	}
	if decoded["library"] != "personal" {
		t.Errorf("library = %v, want personal", decoded["library"])
	}
	if decoded["key"] != "ABC123" {
		t.Errorf("key = %v, want ABC123 (inner result preserved)", decoded["key"])
	}
}

func TestOutputScoped_JSON_PreservesOrder_LibraryFirst(t *testing.T) {
	// Field order matters for human-friendly diffability — library should
	// appear before the result body.
	cmd := &cli.Command{Flags: []cli.Flag{cmdutil.JSONFlag(new(bool))}}
	_ = cmd.Set("json", "true")

	holder := &libraryHolder{Resolved: &zot.LibraryRef{Scope: zot.LibShared}}
	ctx := withLibraryHolder(context.Background(), holder)

	read, restore := captureStdout(t)
	defer restore()
	outputScoped(ctx, cmd, stubResult{Key: "ABC", Title: "T"})
	got := read()

	libIdx := bytes.Index([]byte(got), []byte(`"library"`))
	keyIdx := bytes.Index([]byte(got), []byte(`"key"`))
	if libIdx < 0 || keyIdx < 0 {
		t.Fatalf("missing keys: %q", got)
	}
	if libIdx > keyIdx {
		t.Errorf("library should appear before key in output:\n%s", got)
	}
}

func TestOutputScoped_JSON_DoesNotDuplicateExistingLibrary(t *testing.T) {
	// StatsResult-style: result already has a "library" field. We must not
	// emit a duplicate key.
	cmd := &cli.Command{Flags: []cli.Flag{cmdutil.JSONFlag(new(bool))}}
	_ = cmd.Set("json", "true")

	holder := &libraryHolder{Resolved: &zot.LibraryRef{Scope: zot.LibPersonal}}
	ctx := withLibraryHolder(context.Background(), holder)

	read, restore := captureStdout(t)
	defer restore()
	outputScoped(ctx, cmd, stubResultWithLibrary{Library: "personal", Key: "ABC"})
	got := read()

	count := strings.Count(got, `"library"`)
	if count != 1 {
		t.Errorf(`%d "library" keys, want 1:\n%s`, count, got)
	}
}

func TestOutputScoped_JSON_NoHolder_Passthrough(t *testing.T) {
	// No holder on ctx → outputScoped delegates to cmdutil.Output verbatim.
	cmd := &cli.Command{Flags: []cli.Flag{cmdutil.JSONFlag(new(bool))}}
	_ = cmd.Set("json", "true")

	read, restore := captureStdout(t)
	defer restore()
	outputScoped(context.Background(), cmd, stubResult{Key: "ABC"})
	got := read()

	if strings.Contains(got, `"library"`) {
		t.Errorf(`output should NOT contain "library" when no holder on ctx:\n%s`, got)
	}
}

func TestOutputScoped_Human_PrependsLibraryHeader(t *testing.T) {
	cmd := &cli.Command{} // no --json
	holder := &libraryHolder{Resolved: &zot.LibraryRef{Scope: zot.LibPersonal, Name: "Personal"}}
	ctx := withLibraryHolder(context.Background(), holder)

	read, restore := captureStdout(t)
	defer restore()
	outputScoped(ctx, cmd, stubResult{Key: "ABC", Title: "T"})
	got := read()

	// Strip ANSI for assertions — colors aren't load-bearing here.
	got = stripANSI(got)
	if !strings.Contains(got, "Library: personal") {
		t.Errorf("missing 'Library: personal' header:\n%s", got)
	}
	if !strings.Contains(got, "ABC") {
		t.Errorf("missing inner result body:\n%s", got)
	}
	// Header must come before body.
	libIdx := strings.Index(got, "Library:")
	bodyIdx := strings.Index(got, "ABC")
	if libIdx < 0 || bodyIdx < 0 || libIdx > bodyIdx {
		t.Errorf("header should precede body:\n%s", got)
	}
}

// stripANSI removes \x1b[…m escape sequences so test assertions don't
// depend on terminal styling.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestValidateLibraryBefore_InstallsHolder(t *testing.T) {
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "probe",
				Action: func(ctx context.Context, _ *cli.Command) error {
					h := libraryHolderFromCtx(ctx)
					if h == nil {
						return errors.New("holder missing on ctx")
					}
					if !h.HasFlag {
						return errors.New("HasFlag should be true when --library was passed")
					}
					if h.Partial != zot.LibPersonal {
						return errors.New("Partial = " + string(h.Partial) + ", want personal")
					}
					return errReachedAction
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "--library", "personal", "probe"})
	if !errors.Is(err, errReachedAction) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestValidateLibraryBefore_NoFlag_StillInstallsHolder(t *testing.T) {
	root := &cli.Command{
		Name:   "zot",
		Flags:  PersistentFlags(),
		Before: ValidateLibraryBefore,
		Commands: []*cli.Command{
			{
				Name: "probe",
				Action: func(ctx context.Context, _ *cli.Command) error {
					h := libraryHolderFromCtx(ctx)
					if h == nil {
						return errors.New("holder missing on ctx (must install regardless of --library presence)")
					}
					if h.HasFlag {
						return errors.New("HasFlag should be false when --library was not passed")
					}
					return errReachedAction
				},
			},
		},
	}
	err := root.Run(context.Background(), []string{"zot", "probe"})
	if !errors.Is(err, errReachedAction) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}
