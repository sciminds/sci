package cmdutil

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// nopAction is a do-nothing Action for wiring test trees.
func nopAction(context.Context, *cli.Command) error { return nil }

// testTree builds a small sci-shaped tree with the defaults wired, mirroring
// what buildRoot() does for the real binary.
func testTree(t *testing.T, w io.Writer) *cli.Command {
	t.Helper()
	root := &cli.Command{
		Name:   "sci",
		Writer: w,
		Flags:  []cli.Flag{JSONFlag(new(bool))}, // lint:destination-ok — test harness, never read back
		Commands: []*cli.Command{
			{
				Name: "zot",
				Commands: []*cli.Command{
					{
						Name:   "info",
						Flags:  []cli.Flag{&cli.IntFlag{Name: "limit", Local: true}},
						Action: nopAction,
					},
				},
			},
			{Name: "db", Commands: []*cli.Command{{Name: "convert", Action: nopAction}}},
			{Name: "view", Action: nopAction},
		},
	}
	WireNamespaceDefaults(root)
	return root
}

// --- B1: a malformed flag is a usage error with a clean message ---

// urfave's own handling prints "Incorrect Usage: …" to stderr *and* returns
// the error, so the message renders twice, and the error carries no Code, so
// the process exits 1 where every other misuse exits 2.
func TestFlagParseFailure_IsACodedUsageError(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := testTree(t, &buf)

	err := root.Run(context.Background(), []string{"sci", "zot", "info", "--nosuchflag"})
	if err == nil {
		t.Fatal("an undefined flag should error")
	}
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("want *CodedError so the envelope carries a code, got %T: %v", err, err)
	}
	if coded.Code != CodeUsage {
		t.Errorf("Code = %q, want %q (exit 2 = rewrite the command line)", coded.Code, CodeUsage)
	}
	if !strings.Contains(coded.Message, "nosuchflag") {
		t.Errorf("message should name the flag the user typed, got %q", coded.Message)
	}
	if coded.Try == "" {
		t.Error("an unknown flag should carry a try pointing at --help")
	}
	if strings.Contains(buf.String(), "Incorrect Usage") {
		t.Errorf("urfave's own usage banner must be suppressed (it duplicates the message), got:\n%s", buf.String())
	}
}

// A bad --limit leaks the Go standard library's parser internals into a
// user-facing message. "strconv.ParseInt: parsing …: invalid syntax" tells a
// reader nothing they can act on.
func TestFlagParseFailure_HidesStrconvInternals(t *testing.T) {
	t.Parallel()
	root := testTree(t, io.Discard)

	err := root.Run(context.Background(), []string{"sci", "zot", "info", "--limit", "notanumber"})
	if err == nil {
		t.Fatal("a non-numeric --limit should error")
	}
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("want *CodedError, got %T: %v", err, err)
	}
	if strings.Contains(coded.Message, "strconv") {
		t.Errorf("message leaks Go internals: %q", coded.Message)
	}
	if !strings.Contains(coded.Message, "limit") || !strings.Contains(coded.Message, "notanumber") {
		t.Errorf("message should name the flag and the rejected value, got %q", coded.Message)
	}
	if !strings.Contains(coded.Message, "whole number") {
		t.Errorf("message should say what the flag wants, got %q", coded.Message)
	}
}

// --- JSONRequested: the --json fallback when parsing never got that far ---

func TestJSONRequested(t *testing.T) {
	t.Parallel()
	yes := [][]string{
		{"sci", "zot", "info", "--json"},
		{"sci", "--json", "zot", "info"},
		{"sci", "zot", "info", "--json=true"},
		{"sci", "zot", "info", "-json"},
	}
	for _, args := range yes {
		if !JSONRequested(args) {
			t.Errorf("JSONRequested(%v) = false, want true", args)
		}
	}
	no := [][]string{
		{"sci", "zot", "info"},
		{"sci", "zot", "info", "--json-lines"},
		{"sci", "zot", "info", "--json=false"},
	}
	for _, args := range no {
		if JSONRequested(args) {
			t.Errorf("JSONRequested(%v) = true, want false", args)
		}
	}
}

// --- B2: a namespace parent under --json ---

// `sci zot --json` exiting 0 with a page of ANSI-styled help on stdout is the
// worst shape an agent can get: success, unparseable. It must be the same
// {ok:false,error:{…}} usage envelope every other refusal uses, and it must
// name the subcommands so the caller can recover without a second round trip.
func TestNamespaceUnderJSON_IsACodedUsageError(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := testTree(t, &buf)

	err := root.Run(context.Background(), []string{"sci", "--json", "zot"})
	if err == nil {
		t.Fatal("a bare namespace under --json should error, not print help and exit 0")
	}
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("want *CodedError, got %T: %v", err, err)
	}
	if coded.Code != CodeUsage {
		t.Errorf("Code = %q, want %q", coded.Code, CodeUsage)
	}
	if !strings.Contains(coded.Try, "info") {
		t.Errorf("try should enumerate the subcommands so an agent can recover, got %q", coded.Try)
	}
	if buf.Len() != 0 {
		t.Errorf("--json must leave stdout for the envelope; got %d bytes:\n%s", buf.Len(), buf.String())
	}
}

// The root command is a namespace too — `sci --json` has the same problem.
func TestRootNamespaceUnderJSON_IsACodedUsageError(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := testTree(t, &buf)

	err := root.Run(context.Background(), []string{"sci", "--json"})
	if err == nil {
		t.Fatal("bare `sci --json` should emit an error envelope, not help")
	}
	if _, ok := errors.AsType[*CodedError](err); !ok {
		t.Fatalf("want *CodedError, got %T: %v", err, err)
	}
	if buf.Len() != 0 {
		t.Errorf("--json must leave stdout clean; got:\n%s", buf.String())
	}
}

// Human mode keeps the help page it has always printed, and exits 0.
func TestNamespaceInHumanMode_StillPrintsHelp(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := testTree(t, &buf)

	if err := root.Run(context.Background(), []string{"sci", "zot"}); err != nil {
		t.Fatalf("human mode should still show help and succeed, got: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected the namespace's help page on the writer")
	}
}

// A namespace that has its own Action (`sci setup`, `sci zot doctor`) already
// handles --json itself. Wiring must not steal it.
func TestWireNamespaceDefaults_LeavesAnExistingActionAlone(t *testing.T) {
	t.Parallel()
	var ran bool
	root := &cli.Command{
		Name:   "sci",
		Writer: io.Discard,
		Flags:  []cli.Flag{JSONFlag(new(bool))}, // lint:destination-ok — test harness, never read back
		Commands: []*cli.Command{
			{
				Name:     "setup",
				Action:   func(context.Context, *cli.Command) error { ran = true; return nil },
				Commands: []*cli.Command{{Name: "zot", Action: nopAction}},
			},
		},
	}
	WireNamespaceDefaults(root)

	if err := root.Run(context.Background(), []string{"sci", "--json", "setup"}); err != nil {
		t.Fatalf("mixed Action+Commands should run its own Action, got: %v", err)
	}
	if !ran {
		t.Error("the command's own Action was replaced by the namespace default")
	}
}

// --- B7: `sci <unknown> --help` ---

// With --help, urfave routes to its help topic lookup, which builds
// "No help topic for 'convert'. cloud" — the trailing bare word is a raw
// suggestion concatenated with no template. Without --help the same input is
// handled well; the two paths must not diverge.
func TestUnknownHelpTopic_MatchesTheUnknownCommandPath(t *testing.T) {
	t.Parallel()
	root := testTree(t, io.Discard)

	err := root.Run(context.Background(), []string{"sci", "nosuchthing", "--help"})
	if err == nil {
		t.Fatal("an unknown help topic should error")
	}
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("want *CodedError, got %T: %v", err, err)
	}
	if coded.Code != CodeUsage {
		t.Errorf("Code = %q, want %q", coded.Code, CodeUsage)
	}
	if strings.Contains(coded.Message, "No help topic") {
		t.Errorf("urfave's raw message leaked through: %q", coded.Message)
	}
	if !strings.Contains(coded.Message, "unknown command") || !strings.Contains(coded.Message, "nosuchthing") {
		t.Errorf("message should read like the non-help path, got %q", coded.Message)
	}
	if coded.Try == "" {
		t.Error("the help path should carry the same try as the non-help path")
	}
}

// A known topic must still render its help page and succeed.
func TestKnownHelpTopic_StillRendersHelp(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	root := testTree(t, &buf)

	if err := root.Run(context.Background(), []string{"sci", "zot", "--help"}); err != nil {
		t.Fatalf("a known topic should render help, got: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected help output for a known topic")
	}
}

// --- C3 wired into the real error path ---

func TestRejectUnknownSubcommand_NoMisleadingSuggestion(t *testing.T) {
	t.Parallel()
	root := testTree(t, io.Discard)

	err := root.Run(context.Background(), []string{"sci", "nosuchcommand"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("a distant token should get no suggestion, got: %v", err)
	}
}

func TestRejectUnknownSubcommand_SuggestsNestedPath(t *testing.T) {
	t.Parallel()
	root := testTree(t, io.Discard)

	err := root.Run(context.Background(), []string{"sci", "convert"})
	if err == nil {
		t.Fatal("expected an error")
	}
	coded, ok := errors.AsType[*CodedError](err)
	if !ok {
		t.Fatalf("want *CodedError, got %T: %v", err, err)
	}
	if !strings.Contains(coded.Try, "sci db convert") {
		t.Errorf("try should point at the nested command that does exist, got %q", coded.Try)
	}
}
