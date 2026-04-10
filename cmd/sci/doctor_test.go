package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/ui"
)

func TestToolsReccs_JSONSkipsForm(t *testing.T) {
	ui.SetQuiet(false)
	root := buildRoot()

	// --json mode should not launch the interactive multi-select.
	// It will fail trying to access brew but should NOT fail on huh form.
	err := root.Run(context.Background(), []string{"sci", "--json", "tools", "reccs"})

	// We expect either success or a brew-related error, NOT a TTY/huh error.
	if err != nil && strings.Contains(err.Error(), "TTY") {
		t.Errorf("--json mode should not open a TTY, got: %v", err)
	}
	ui.SetQuiet(false)
}

func TestDoctor_JSONIncludesBrewfileFields(t *testing.T) {
	// Capture stdout to parse the JSON output.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	root := buildRoot()
	// doctor --json may exit(1) if checks fail, but it should not panic or
	// skip the Brewfile/tools sections.
	_ = root.Run(context.Background(), []string{"sci", "--json", "doctor"})

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON from doctor --json, got: %s", buf.String())
	}

	// The key assertion: --json must include Brewfile/tools fields, not
	// bail early after pre-flight checks.
	if _, ok := result["brewfile_path"]; !ok {
		// brewfile_path is omitempty so it may be absent if Homebrew isn't
		// installed, but tools should always be present.
		if _, hasTools := result["tools"]; !hasTools {
			t.Error("doctor --json output missing both brewfile_path and tools — " +
				"likely still exiting early after pre-flight checks")
		}
	}
}
