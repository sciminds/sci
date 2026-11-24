package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/version"
)

// root_contract_test.go — the agent-facing contract of the root command:
// every invocation either produces an answer or a parseable refusal, and the
// binary can always say which build it is.

// --- B6: the version has to be reachable ---

func TestVersion_FlagIsAccepted(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf

	if err := root.Run(context.Background(), []string{"sci", "--version"}); err != nil {
		t.Fatalf("`sci --version` should succeed, got: %v", err)
	}
}

func TestVersion_CommandExists(t *testing.T) {
	root := buildRoot()
	if findCmd(root.Commands, "version") == nil {
		t.Fatal("`sci version` should be a real command — it is what people type first")
	}
	if err := root.Run(context.Background(), []string{"sci", "version"}); err != nil {
		t.Fatalf("`sci version` should succeed, got: %v", err)
	}
}

// The version is only useful in a bug report if a script can lift it out.
func TestVersion_ResultCarriesTheBuildIdentity(t *testing.T) {
	origCommit, origVersion, origDev := version.Commit, version.Version, version.Dev
	version.Commit, version.Version, version.Dev = "1234567abcdef89", "v2026.08.03", "false"
	t.Cleanup(func() {
		version.Commit, version.Version, version.Dev = origCommit, origVersion, origDev
	})

	data, err := json.Marshal(versionResult{}.JSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["version"] != "v2026.08.03" {
		t.Errorf("version = %v, want v2026.08.03", got["version"])
	}
	if got["commit"] != "1234567abcdef89" {
		t.Errorf("commit = %v", got["commit"])
	}
	if got["dev"] != false {
		t.Errorf("dev = %v, want false for a stamped release", got["dev"])
	}
	if !strings.Contains(versionResult{}.Human(), "v2026.08.03") {
		t.Errorf("human form should name the version, got %q", versionResult{}.Human())
	}
}

// --- B2: namespace parents under --json ---

// A namespace parent under --json used to exit 0 with a page of ANSI-styled
// help on stdout — a success code attached to output no consumer can parse,
// which is the one failure mode an agent cannot detect or recover from.
//
// The list is explicit rather than derived from the tree: by the time
// buildRoot() returns, WireNamespaceDefaults has already given every pure
// namespace an Action, so "has no Action of its own" is no longer visible.
// `sci cloud` / `sci lab` share the behavior but their Before
// hooks reach the network, so they stay out of the gate.
func TestEveryNamespaceRefusesUnderJSON(t *testing.T) {
	paths := []string{
		"sci",
		"sci db", "sci py", "sci proj", "sci vid",
		"sci zot", "sci zot item", "sci zot notes", "sci zot tags",
		"sci zot collection", "sci zot link",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			r := buildRoot()
			var buf strings.Builder
			r.Writer = &buf

			argv := append(strings.Split(path, " "), "--json")
			err := r.Run(context.Background(), argv)
			if err == nil {
				t.Fatalf("%s --json: expected a usage envelope, got success with output:\n%s", path, buf.String())
			}
			if buf.Len() != 0 {
				t.Errorf("%s --json: stdout must carry only the envelope, got:\n%s", path, buf.String())
			}
			if !strings.Contains(err.Error(), "command group") {
				t.Errorf("%s --json: message should explain it is a group, got: %v", path, err)
			}
		})
	}
}

// --- B7 / C3 on the real tree ---

func TestUnknownCommandWithHelpFlag_ReadsLikeTheNonHelpPath(t *testing.T) {
	root := buildRoot()
	root.Writer = &strings.Builder{}

	err := root.Run(context.Background(), []string{"sci", "nosuchthing", "--help"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "No help topic") {
		t.Errorf("urfave's raw help-topic message leaked: %v", err)
	}
	if !strings.Contains(err.Error(), `unknown command "nosuchthing"`) {
		t.Errorf("message should match the non-help path, got: %v", err)
	}
}

func TestTypoSuggestions_OnTheRealTree(t *testing.T) {
	cases := []struct {
		typed, want string
	}{
		{"zto", `"zot"`},
		{"viwe", `"view"`},
		{"doctr", `"doctor"`},
	}
	for _, tt := range cases {
		t.Run(tt.typed, func(t *testing.T) {
			root := buildRoot()
			root.Writer = &strings.Builder{}
			err := root.Run(context.Background(), []string{"sci", tt.typed})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("`sci %s` should suggest %s, got: %v", tt.typed, tt.want, err)
			}
		})
	}
}

func TestNoSuggestionForUnrecognizableInput(t *testing.T) {
	root := buildRoot()
	root.Writer = &strings.Builder{}
	err := root.Run(context.Background(), []string{"sci", "nosuchcommand"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("a token resembling nothing should get no suggestion, got: %v", err)
	}
}

// `sci convert` is plausible input: `sci db convert`, `sci py convert` and
// `sci vid convert` all exist. Nothing at the top level is close, so the only
// honest answer is the deeper paths.
func TestNestedCommandNameIsPointedAt(t *testing.T) {
	root := buildRoot()
	root.Writer = &strings.Builder{}
	err := root.Run(context.Background(), []string{"sci", "convert"})
	if err == nil {
		t.Fatal("expected an error")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
	}
	if !strings.Contains(coded.Try, "sci db convert") {
		t.Errorf("try should point at the nested commands, got %q", coded.Try)
	}
}

// --- B1: a malformed flag ---

func TestMalformedFlag_IsAUsageErrorWithoutTheBanner(t *testing.T) {
	root := buildRoot()
	var buf strings.Builder
	root.Writer = &buf

	err := root.Run(context.Background(), []string{"sci", "db", "info", "--nosuchflag"})
	if err == nil {
		t.Fatal("expected an error for an undefined flag")
	}
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want *cmdutil.CodedError, got %T: %v", err, err)
	}
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("Code = %q, want usage (exit 2)", coded.Code)
	}
	if strings.Contains(buf.String(), "Incorrect Usage") {
		t.Errorf("urfave's duplicate banner should be suppressed, got:\n%s", buf.String())
	}
}
