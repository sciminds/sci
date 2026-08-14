package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	projnew "github.com/sciminds/sci/internal/proj/new"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

// TestProjRunHelpIsReachable — `sci proj run` sets SkipFlagParsing so a
// task's own flags reach the task runner, which also means `--help`
// arrives as args[0] and used to be dispatched as if it were a task name:
// the user got "no project detected in …" instead of the command's help.
// It was the only leaf in the tree whose help could not be obtained.
func TestProjRunHelpIsReachable(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			var out bytes.Buffer
			root := buildRoot()
			root.Writer = &out

			if err := root.Run(context.Background(), []string{"sci", "proj", "run", flag}); err != nil {
				t.Fatalf("`sci proj run %s` should print help and succeed, got %v", flag, err)
			}
			for _, want := range []string{"<task> [args...]", "sci proj run test", "sci proj run lint --fix"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help must carry %q; got:\n%s", want, out.String())
				}
			}
		})
	}
}

// TestProjRunForwardsNonLeadingFlags is the other half: only a LEADING
// help flag is the command's own. `sci proj run lint --help` is asking the
// lint task for its help, and swallowing it here would break the
// pass-through that SkipFlagParsing exists for.
func TestProjRunForwardsNonLeadingFlags(t *testing.T) {
	var out bytes.Buffer
	root := buildRoot()
	root.Writer = &out

	// No project in cmd/sci, so the task dispatch fails — which is itself
	// the evidence that --help was forwarded rather than intercepted.
	err := root.Run(context.Background(), []string{"sci", "proj", "run", "lint", "--help"})
	if err == nil {
		t.Fatal("`sci proj run lint --help` must reach the task runner, not print sci's help")
	}
	if out.Len() > 0 {
		t.Errorf("sci printed its own help for a trailing --help:\n%s", out.String())
	}
}

func TestProjNew_JSONRequiresNameArg(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	root := buildRoot()

	// --json with no name argument should error instead of launching wizard.
	err := root.Run(context.Background(), []string{"sci", "--json", "proj", "new"})

	if err == nil {
		t.Fatal("expected error when --json is set without a name argument")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention project name, got: %v", err)
	}
}

func TestProjNew_WritingDryRunSmoke(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	root := buildRoot()
	err := root.Run(context.Background(), []string{
		"sci", "--json", "proj", "new", "paper-test",
		"--kind", "writing", "--dry-run",
		"--author", "Test", "--email", "t@e.com",
	})
	if err != nil {
		t.Fatalf("dry-run with --kind writing failed: %v", err)
	}
}

func TestProjNew_MdLayoutTemplateFlags(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	// Tap the real `proj new` Action so we can assert the flag → CreateOptions
	// wiring on the live *cli.Command, then still run the command for real.
	root := buildRoot()
	newCmd := findCmd(findCmd(root.Commands, "proj").Commands, "new")
	if newCmd == nil {
		t.Fatal("proj new command not found")
	}
	inner := newCmd.Action
	var got projnew.CreateOptions
	newCmd.Action = func(ctx context.Context, cmd *cli.Command) error {
		got = projNewOptions(cmd)
		return inner(ctx, cmd)
	}

	err := root.Run(context.Background(), []string{
		"sci", "--json", "proj", "new", "paper-flags",
		"--kind", "writing", "--md-layout", "composed", "--template", "default",
		"--dry-run", "--author", "Test", "--email", "t@e.com",
	})
	if err != nil {
		t.Fatalf("dry-run with --md-layout composed --template default failed: %v", err)
	}
	if got.MdLayout != "composed" {
		t.Errorf("expected --md-layout to set MdLayout=composed, got %q", got.MdLayout)
	}
	if got.Template != "default" {
		t.Errorf("expected --template to set Template=default, got %q", got.Template)
	}
	if got.Kind != "writing" {
		t.Errorf("expected --kind to set Kind=writing, got %q", got.Kind)
	}
	if !got.DryRun {
		t.Error("expected --dry-run to set DryRun=true")
	}
}
