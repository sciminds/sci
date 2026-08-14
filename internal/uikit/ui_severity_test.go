package uikit

import "testing"

func TestSeverity_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level Severity
		want  string
	}{
		{SeverityInfo, "info"},
		{SeveritySuccess, "success"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestSeverity_Icon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level Severity
		want  string
	}{
		{SeverityInfo, IconArrow},
		{SeveritySuccess, IconPass},
		{SeverityWarning, IconWarn},
		{SeverityError, IconFail},
	}
	for _, tt := range tests {
		if got := tt.level.Icon(); got != tt.want {
			t.Errorf("Severity(%d).Icon() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// TestSeverity_StyleIsDistinctPerLevel guards the thing an alias makes easy to
// break: browser.StatusKind is Severity, so a level that silently fell through
// to the same style as another would make two different outcomes — a success
// and a failure — render identically in every list status bar.
func TestSeverity_StyleIsDistinctPerLevel(t *testing.T) {
	t.Parallel()
	const sample = "message"
	seen := map[string]Severity{}
	for _, l := range []Severity{SeverityInfo, SeveritySuccess, SeverityWarning, SeverityError} {
		got := l.Style().Render(sample)
		if prev, dup := seen[got]; dup {
			t.Errorf("%v and %v render identically (%q) — a status bar cannot distinguish them", prev, l, got)
		}
		seen[got] = l
	}
}
