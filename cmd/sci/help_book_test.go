package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/help"
)

// TestEveryCommandBookHasAnOverview pins the one thing `sci help` cannot
// derive from the command tree.
//
// A book's name, its one-line usage and its subcommand list all come from
// the live tree, so they follow a rename for free. The wiki-style
// overview above them does not — it is hand-written in
// internal/help/descs.go and keyed by command name, and a missing key is
// a plain map miss that renders as an empty block. That is how `sci
// setup` shipped as the only Getting Started command with no description
// at all: nothing failed, the panel was simply blank.
func TestEveryCommandBookHasAnOverview(t *testing.T) {
	for _, g := range help.BuildGroups(buildRoot()) {
		if strings.TrimSpace(g.LongDesc) == "" {
			t.Errorf("`sci %s` has no overview — add a longDescs entry in internal/help/descs.go", g.Name)
		}
	}
}

// TestVersionIsAVisibleCommandWithABook pins the pair that has to move
// together: `sci version` can only be visible once internal/help/descs.go
// carries an overview for it, because a visible top-level command with no
// entry renders as an empty panel (that is what
// TestEveryCommandBookHasAnOverview guards). Hiding the command was the
// stopgap; the book entry is the fix.
func TestVersionIsAVisibleCommandWithABook(t *testing.T) {
	root := buildRoot()
	cmd := findCmd(root.Commands, "version")
	if cmd == nil {
		t.Fatal("`sci version` should be registered")
	}
	if cmd.Hidden {
		t.Error("`sci version` should be visible — it is what people type first")
	}
	if help.FindGroup(help.BuildGroups(root), "version") == nil {
		t.Error("`sci version` should appear as a book in `sci help`")
	}
}

// ── B8: `sci help` must not swallow its own flags ──────────────────────────
//
// urfave/cli routes a command *named* "help" straight to its Action with the
// raw argv tail (command_run.go: `cmd.Name == helpName` skips pre-parse), so
// `--help`, `--json` and a mistyped topic all arrive as positional args. Left
// alone, every one of them came back as "unknown command: --help" at exit 1,
// and a non-TTY run reached the user as bubbletea's raw "error opening TTY".

func TestHelpFlagRendersHelpInsteadOfFailing(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf

	if err := root.Run(context.Background(), []string{"sci", "help", "--help"}); err != nil {
		t.Fatalf("`sci help --help` should render help, got: %v", err)
	}
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "sci help") {
		t.Errorf("the help page should name the command, got:\n%s", out)
	}
}

func TestHelpRefusesJSONAndNamesAScriptableAlternative(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf

	err := root.Run(context.Background(), []string{"sci", "help", "--json"})
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage (exit 2)", coded.Code)
	}
	if !strings.Contains(coded.Try, "--help") {
		t.Errorf("try should name the scriptable alternative, got %q", coded.Try)
	}
	if buf.Len() != 0 {
		t.Errorf("stdout must carry only the envelope, got:\n%s", buf.String())
	}
}

func TestHelpWithoutATerminalRefusesInsteadOfCrashing(t *testing.T) {
	orig := helpIsInteractive
	helpIsInteractive = func() bool { return false }
	t.Cleanup(func() { helpIsInteractive = orig })

	for _, argv := range [][]string{
		{"sci", "help"},
		{"sci", "help", "zot"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			root := buildRoot()
			root.Writer = &strings.Builder{}

			err := root.Run(context.Background(), argv)
			coded, ok := errors.AsType[*cmdutil.CodedError](err)
			if !ok {
				t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("Code = %q, want usage (exit 2)", coded.Code)
			}
			if strings.Contains(coded.Message, "bubbletea") {
				t.Errorf("the library error should never reach the user, got %q", coded.Message)
			}
			if coded.Try == "" {
				t.Error("a refusal must name something the caller can run instead")
			}
		})
	}
}

func TestHelpUnknownTopicIsAUsageErrorWithASuggestion(t *testing.T) {
	root := buildRoot()
	root.Writer = &strings.Builder{}

	err := root.Run(context.Background(), []string{"sci", "help", "nonexistentthing"})
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage (exit 2)", coded.Code)
	}
	if !strings.Contains(coded.Message, "nonexistentthing") {
		t.Errorf("the message should name what was typed, got %q", coded.Message)
	}
	if !strings.Contains(coded.Try, "zot") {
		t.Errorf("try should list the topics that do exist, got %q", coded.Try)
	}
}

// A near-miss gets the same treatment every other mistyped name gets in this
// CLI: the token, and the name we think was meant.
func TestHelpNearMissTopicIsSuggestedByName(t *testing.T) {
	root := buildRoot()
	root.Writer = &strings.Builder{}

	err := root.Run(context.Background(), []string{"sci", "help", "zo"})
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
	}
	if !strings.Contains(coded.Try, `"zot"`) {
		t.Errorf("try should suggest zot, got %q", coded.Try)
	}
}

// ── I1 (help half): --help output is human output ──────────────────────────

// TestHelpOutputHonorsTheDestination pins that `--help` pages go through
// [cmdutil.HumanWriter] like every other human-mode stream: stripped when the
// destination is not a terminal, still colored when CLICOLOR_FORCE says so.
//
// Command *results* have honored this since HumanWriter landed; help pages
// went to urfave's raw writer, so `NO_COLOR=1 sci zot --help | …` carried 27
// escape sequences into whatever read it.
func TestHelpOutputHonorsTheDestination(t *testing.T) {
	t.Run("stripped when piped", func(t *testing.T) {
		out := captureStdout(t, func() {
			root := buildRoot()
			if err := root.Run(context.Background(), []string{"sci", "zot", "--help"}); err != nil {
				t.Errorf("`sci zot --help` should succeed, got: %v", err)
			}
		})
		if strings.Contains(out, "\x1b") {
			t.Errorf("piped help must carry no escape sequences, got:\n%q", out)
		}
		if !strings.Contains(out, "Usage") {
			t.Errorf("help should still render its sections, got:\n%s", out)
		}
	})

	t.Run("colored when forced", func(t *testing.T) {
		t.Setenv("CLICOLOR_FORCE", "1")
		t.Setenv("TERM", "xterm-256color")
		out := captureStdout(t, func() {
			root := buildRoot()
			if err := root.Run(context.Background(), []string{"sci", "zot", "--help"}); err != nil {
				t.Errorf("`sci zot --help` should succeed, got: %v", err)
			}
		})
		if !strings.Contains(out, "\x1b") {
			t.Error("CLICOLOR_FORCE=1 should still produce color — the writer routes, it does not strip")
		}
	})
}

// captureStdout swaps os.Stdout for a pipe while fn runs and returns what was
// written to it. The help writer resolves [cmdutil.HumanWriter] per write, so
// the swap is visible to it — which is exactly the property that keeps a
// cached writer from being an option here.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	os.Stdout = orig
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
