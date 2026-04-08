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
