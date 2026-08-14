package main

import (
	"context"
	"strings"
	"testing"

	projnew "github.com/sciminds/sci/internal/proj/new"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

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
