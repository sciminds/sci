package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestUnwrapZoteroDiv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, want string
	}{
		{
			"wrapped markdown",
			`<div class="zotero-note znv1">---\nkey: val\n---</div>`,
			`---\nkey: val\n---`,
		},
		{
			"wrapped html",
			`<div class="zotero-note znv1"><h1>Title</h1></div>`,
			`<h1>Title</h1>`,
		},
		{"no wrapper", "# Heading\nContent", "# Heading\nContent"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := local.UnwrapZoteroDiv(tc.body); got != tc.want {
				t.Errorf("UnwrapZoteroDiv(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestIsHTMLNote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"html paragraph", "<p>Hello</p>", true},
		{"html div", "<div>content</div>", true},
		{"html leading whitespace", "  <div>content</div>", true},
		{"yaml frontmatter", "---\nkey: val\n---\n# Heading", false},
		{"bare heading", "# Heading", false},
		{"plain text", "Just some text", false},
		{"empty string", "", false},
		{
			"zotero-wrapped markdown",
			`<div class="zotero-note znv1">---` + "\n" + `key: val` + "\n" + `---</div>`,
			false,
		},
		{
			"zotero-wrapped html",
			`<div class="zotero-note znv1"><h1>Title</h1></div>`,
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isHTMLNote(tc.body); got != tc.want {
				t.Errorf("isHTMLNote(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestRunMQ(t *testing.T) {
	t.Parallel()
	if os.Getenv("MQ") == "" {
		t.Skip("MQ=1 not set — skipping mq integration test")
	}
	mqBin, err := exec.LookPath("mq")
	if err != nil {
		t.Fatalf("mq not found: %v", err)
	}

	dir := t.TempDir()
	mdFile := filepath.Join(dir, "test.md")
	content := "# Introduction\n\nHello world.\n\n## Methods\n\nWe used transformers.\n"
	if err := os.WriteFile(mdFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Test: extract all headings.
	out, err := runMQ(context.Background(), mqBin, []string{".h"}, mdFile)
	if err != nil {
		t.Fatalf("runMQ .h: %v", err)
	}
	if !strings.Contains(out, "# Introduction") {
		t.Errorf("missing h1 in output: %s", out)
	}
	if !strings.Contains(out, "## Methods") {
		t.Errorf("missing h2 in output: %s", out)
	}
}

func TestRunMQ_JSONOutput(t *testing.T) {
	t.Parallel()
	if os.Getenv("MQ") == "" {
		t.Skip("MQ=1 not set — skipping mq integration test")
	}
	mqBin, err := exec.LookPath("mq")
	if err != nil {
		t.Fatalf("mq not found: %v", err)
	}

	dir := t.TempDir()
	mdFile := filepath.Join(dir, "test.md")
	content := "# Title\n\nParagraph.\n"
	if err := os.WriteFile(mdFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runMQ(context.Background(), mqBin, []string{"-F", "json", ".h"}, mdFile)
	if err != nil {
		t.Fatalf("runMQ -F json: %v", err)
	}
	if !strings.Contains(out, `"type"`) {
		t.Errorf("expected JSON output, got: %s", out)
	}
}

func TestRunMQ_SectionModule(t *testing.T) {
	t.Parallel()
	if os.Getenv("MQ") == "" {
		t.Skip("MQ=1 not set — skipping mq integration test")
	}
	mqBin, err := exec.LookPath("mq")
	if err != nil {
		t.Fatalf("mq not found: %v", err)
	}

	dir := t.TempDir()
	mdFile := filepath.Join(dir, "test.md")
	content := "# Intro\n\nContent.\n\n## Methods\n\nWe used transformers.\n\n## Results\n\n95% accuracy.\n"
	if err := os.WriteFile(mdFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runMQ(context.Background(), mqBin, []string{"-m", "section", `section::section("Methods")`}, mdFile)
	if err != nil {
		t.Fatalf("runMQ section: %v", err)
	}
	if !strings.Contains(out, "Methods") {
		t.Errorf("expected Methods section in output: %s", out)
	}
}

func TestLLMQueryRequiresFilter(t *testing.T) {
	t.Parallel()
	// Invoke the action directly with no flags set — should fail before
	// touching the DB.
	llmQuerySearch = ""
	llmQueryTag = ""
	llmQueryCollection = ""
	llmQueryKey = nil
	err := llmQueryAction(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error when no filter is provided")
	}
	if !strings.Contains(err.Error(), "at least one filter is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveMQ(t *testing.T) {
	t.Parallel()
	// This test just verifies the function doesn't panic and returns
	// a reasonable result regardless of whether mq is installed.
	bin, err := resolveMQ()
	if err != nil {
		if !strings.Contains(err.Error(), "mq binary not found") {
			t.Errorf("unexpected error message: %v", err)
		}
	} else if bin == "" {
		t.Error("resolveMQ returned empty path with nil error")
	}
}
