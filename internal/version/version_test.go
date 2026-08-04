package version

import "testing"

func TestString(t *testing.T) {
	tests := []struct {
		name            string
		version, commit string
		want            string
	}{
		{"release build", "v2026.08.03", "1234567abcdef89", "v2026.08.03 (1234567)"},
		{"dev build", "", "1234567abcdef89", "dev (1234567)"},
		{"short commit stays whole", "v2026.08.03.1", "abc12", "v2026.08.03.1 (abc12)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldVersion, oldCommit := Version, Commit
			Version, Commit = tt.version, tt.commit
			defer func() { Version, Commit = oldVersion, oldCommit }()

			if got := String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
