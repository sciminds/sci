package brew

import (
	"errors"
	"testing"
)

// mockProber records calls and returns preset results.
type mockProber struct {
	formulaResult bool
	formulaErr    error
	caskResult    bool
	caskErr       error
	pypiResult    bool
	pypiErr       error
}

func (m *mockProber) ProbeFormula(pkg string) (bool, error) {
	return m.formulaResult, m.formulaErr
}
func (m *mockProber) ProbeCask(pkg string) (bool, error) {
	return m.caskResult, m.caskErr
}
func (m *mockProber) ProbePyPI(pkg string) (bool, error) {
	return m.pypiResult, m.pypiErr
}

func TestDetect_FormulaOnly(t *testing.T) {
	p := &mockProber{formulaResult: true}
	matches, err := Detect(p, "htop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Type != "formula" {
		t.Errorf("match type = %q, want %q", matches[0].Type, "formula")
	}
	if matches[0].Name != "htop" {
		t.Errorf("match name = %q, want %q", matches[0].Name, "htop")
	}
}

func TestDetect_CaskOnly(t *testing.T) {
	p := &mockProber{caskResult: true}
	matches, err := Detect(p, "firefox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Type != "cask" {
		t.Errorf("match type = %q, want %q", matches[0].Type, "cask")
	}
}

func TestDetect_UVOnly(t *testing.T) {
	p := &mockProber{pypiResult: true}
	matches, err := Detect(p, "marimo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Type != "uv" {
		t.Errorf("match type = %q, want %q", matches[0].Type, "uv")
	}
}

func TestDetect_MultipleMatches_PriorityOrder(t *testing.T) {
	p := &mockProber{formulaResult: true, caskResult: true, pypiResult: true}
	matches, err := Detect(p, "rg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(matches))
	}
	// Priority: formula > cask > uv
	if matches[0].Type != "formula" {
		t.Errorf("matches[0].Type = %q, want %q", matches[0].Type, "formula")
	}
	if matches[1].Type != "cask" {
		t.Errorf("matches[1].Type = %q, want %q", matches[1].Type, "cask")
	}
	if matches[2].Type != "uv" {
		t.Errorf("matches[2].Type = %q, want %q", matches[2].Type, "uv")
	}
}

func TestDetect_FormulaCask_PriorityOrder(t *testing.T) {
	p := &mockProber{formulaResult: true, caskResult: true}
	matches, err := Detect(p, "firefox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	if matches[0].Type != "formula" {
		t.Errorf("matches[0].Type = %q, want %q", matches[0].Type, "formula")
	}
	if matches[1].Type != "cask" {
		t.Errorf("matches[1].Type = %q, want %q", matches[1].Type, "cask")
	}
}

func TestDetect_NoMatch(t *testing.T) {
	p := &mockProber{}
	matches, err := Detect(p, "notarealpackage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestDetect_ProbeErrorsAreNotFatal(t *testing.T) {
	// If a probe errors, it's treated as "not found" — not a fatal error.
	p := &mockProber{
		formulaErr: errors.New("brew not responding"),
		caskResult: true,
	}
	matches, err := Detect(p, "firefox")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Type != "cask" {
		t.Errorf("match type = %q, want %q", matches[0].Type, "cask")
	}
}

func TestDetect_AllProbesFail(t *testing.T) {
	// All probes error → still no fatal error, just 0 matches.
	p := &mockProber{
		formulaErr: errors.New("fail"),
		caskErr:    errors.New("fail"),
		pypiErr:    errors.New("fail"),
	}
	matches, err := Detect(p, "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}
