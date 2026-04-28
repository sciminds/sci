package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/uikit"
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

	// Reset shared state — these globals leak across tests since flag
	// destinations are package-level.
	t.Cleanup(func() {
		projNewKind = ""
		projNewDryRun = false
	})

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

	t.Cleanup(func() {
		projNewKind = ""
		projNewMdLayout = ""
		projNewTemplate = ""
		projNewDryRun = false
	})

	root := buildRoot()
	err := root.Run(context.Background(), []string{
		"sci", "--json", "proj", "new", "paper-flags",
		"--kind", "writing", "--md-layout", "composed", "--template", "default",
		"--dry-run", "--author", "Test", "--email", "t@e.com",
	})
	if err != nil {
		t.Fatalf("dry-run with --md-layout composed --template default failed: %v", err)
	}
	if projNewMdLayout != "composed" {
		t.Errorf("expected --md-layout to set projNewMdLayout=composed, got %q", projNewMdLayout)
	}
	if projNewTemplate != "default" {
		t.Errorf("expected --template to set projNewTemplate=default, got %q", projNewTemplate)
	}
}
