package data

import (
	"testing"
)

func TestIsSafeIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"penguins", true},
		{"BOLD signal", true},
		{"my_table", true},
		{"Table123", true},
		{"", false},
		{"drop;--", false},
		{`table"name`, false},
		{"hello\tworld", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsSafeIdentifier(tt.input); got != tt.want {
				t.Errorf("IsSafeIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
