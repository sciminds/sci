package doctor

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHuman_ShowsSections(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Xcode CLT", Status: StatusPass, Message: "installed"},
				{Label: "Shell", Status: StatusPass, Message: "zsh"},
			}},
			{Name: "Identity", Checks: []CheckResult{
				{Label: "Git user.name", Status: StatusPass, Message: "Test User"},
			}},
		},
	}

	out := r.Human()
	if !strings.Contains(out, "Pre-flight") {
		t.Error("expected Pre-flight section")
	}
	if !strings.Contains(out, "Identity") {
		t.Error("expected Identity section")
	}
	if strings.Contains(out, "passed") {
		t.Error("should not show 'passed' summary when everything passes")
	}
}

func TestHuman_ToolsSummaryLine(t *testing.T) {
	r := DocResult{
		Tools: []ToolInfo{
			{Name: "duckdb", Installed: true},
			{Name: "uv", Installed: true},
			{Name: "ffmpeg", Installed: false},
		},
	}

	out := r.Human()
	if !strings.Contains(out, "2/3") {
		t.Error("expected '2/3' tools summary")
	}
}

func TestJSON_HasToolsAndSummary(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Xcode CLT", Status: StatusPass, Message: "installed"},
			}},
		},
		Tools: []ToolInfo{
			{Name: "duckdb", Installed: true},
		},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"name":"duckdb"`) {
		t.Error("expected tool name in JSON output")
	}
	if !strings.Contains(s, `"pass":1`) {
		t.Error("expected pass:1 in JSON summary")
	}
}

func TestHuman_FailWarnSummaryLine(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Xcode CLT", Status: StatusFail, Message: "not installed"},
				{Label: "Shell", Status: StatusWarn, Message: "bash — expected zsh"},
				{Label: "Other", Status: StatusPass, Message: "ok"},
			}},
		},
	}

	out := r.Human()

	if !strings.Contains(out, "1 failed") {
		t.Error("expected '1 failed' in summary line")
	}
	if !strings.Contains(out, "1 warnings") {
		t.Error("expected '1 warnings' in summary line")
	}
}

func TestHuman_EmptyResult(t *testing.T) {
	r := DocResult{}
	out := r.Human()
	if out == "" {
		t.Error("expected non-empty output even for empty result (trailing newline)")
	}
}

func TestJSON_FailAndWarnCounts(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Test", Checks: []CheckResult{
				{Label: "a", Status: StatusPass},
				{Label: "b", Status: StatusFail},
				{Label: "c", Status: StatusWarn},
				{Label: "d", Status: StatusFail},
			}},
		},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"pass":1`) {
		t.Errorf("expected pass:1, got %s", s)
	}
	if !strings.Contains(s, `"fail":2`) {
		t.Errorf("expected fail:2, got %s", s)
	}
	if !strings.Contains(s, `"warn":1`) {
		t.Errorf("expected warn:1, got %s", s)
	}
}

func TestJSON_EmptyTools(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Test", Checks: []CheckResult{
				{Label: "a", Status: StatusPass, Message: "ok"},
			}},
		},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"tools":[{`) {
		t.Error("expected tools to be omitted or null when empty")
	}
}

func TestJSON_IncludesBrewfileFields(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		BrewfilePath:    "/Users/test/.Brewfile",
		BrewfileCreated: true,
		PackagesAdded:   []string{"pixi", "uv"},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"brewfile_path":"/Users/test/.Brewfile"`) {
		t.Errorf("expected brewfile_path in JSON, got %s", s)
	}
	if !strings.Contains(s, `"brewfile_created":true`) {
		t.Errorf("expected brewfile_created:true in JSON, got %s", s)
	}
	if !strings.Contains(s, `"packages_added":["pixi","uv"]`) {
		t.Errorf("expected packages_added in JSON, got %s", s)
	}
}

func TestJSON_IncludesInstallFields(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		Tools: []ToolInfo{
			{Name: "git", Installed: true},
			{Name: "uv", Installed: false},
		},
		ToolsInstalled: []string{"uv"},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"tools_installed":["uv"]`) {
		t.Errorf("expected tools_installed in JSON, got %s", s)
	}
}

func TestJSON_IncludesInstallError(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		InstallError: "brew bundle failed",
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"install_error":"brew bundle failed"`) {
		t.Errorf("expected install_error in JSON, got %s", s)
	}
}

func TestJSON_OmitsEmptyBrewfileFields(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	// When empty, these fields should be omitted from JSON
	if strings.Contains(s, `"brewfile_path"`) {
		t.Errorf("expected brewfile_path to be omitted when empty, got %s", s)
	}
	if strings.Contains(s, `"packages_added"`) {
		t.Errorf("expected packages_added to be omitted when empty, got %s", s)
	}
	if strings.Contains(s, `"tools_installed"`) {
		t.Errorf("expected tools_installed to be omitted when empty, got %s", s)
	}
	if strings.Contains(s, `"install_error"`) {
		t.Errorf("expected install_error to be omitted when empty, got %s", s)
	}
}

func TestJSON_IncludesToolCheckError(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		ToolCheckError: "brew: command not found",
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"tool_check_error":"brew: command not found"`) {
		t.Errorf("expected tool_check_error in JSON, got %s", s)
	}
}

func TestJSON_IncludesAppendError(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		AppendError: "permission denied",
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if !strings.Contains(s, `"append_error":"permission denied"`) {
		t.Errorf("expected append_error in JSON, got %s", s)
	}
}

func TestHuman_ToolCheckError(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
		ToolCheckError: "brew borked",
	}

	output := r.Human()
	if !strings.Contains(output, "could not check") {
		t.Errorf("expected 'could not check' warning in Human output, got:\n%s", output)
	}
	if !strings.Contains(output, "brew borked") {
		t.Errorf("expected error message in Human output, got:\n%s", output)
	}
}

func TestJSON_OmitsNewErrorFieldsWhenEmpty(t *testing.T) {
	r := DocResult{
		Sections: []CheckSection{
			{Name: "Pre-flight", Checks: []CheckResult{
				{Label: "Homebrew", Status: StatusPass, Message: "installed"},
			}},
		},
	}

	raw, err := json.Marshal(r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)

	if strings.Contains(s, `"tool_check_error"`) {
		t.Errorf("expected tool_check_error to be omitted when empty, got %s", s)
	}
	if strings.Contains(s, `"append_error"`) {
		t.Errorf("expected append_error to be omitted when empty, got %s", s)
	}
}

func TestAllPassed(t *testing.T) {
	passing := DocResult{Sections: []CheckSection{
		{Checks: []CheckResult{{Status: StatusPass}, {Status: StatusWarn}}},
	}}
	if !passing.AllPassed() {
		t.Error("expected AllPassed=true with pass+warn")
	}

	failing := DocResult{Sections: []CheckSection{
		{Checks: []CheckResult{{Status: StatusPass}, {Status: StatusFail}}},
	}}
	if failing.AllPassed() {
		t.Error("expected AllPassed=false with a fail")
	}
}
