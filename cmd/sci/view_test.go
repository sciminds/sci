package main

import "testing"

func TestIsMarkdown(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"notes.md", true},
		{"NOTES.MD", true},
		{"docs/readme.markdown", true},
		{"data.csv", false},
		{"results.json", false},
		{"experiment.db", false},
		{"plain", false},
		{"weird.mdx", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := isMarkdown(tc.path); got != tc.want {
				t.Errorf("isMarkdown(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
