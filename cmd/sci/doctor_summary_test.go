package main

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/doctor"
)

// TestClosingSummary verifies the trailing message picks between the
// celebratory banner and the "finish these logins" warning block, driven by
// the gh/hf identity check statuses already gathered earlier in the run.
func TestClosingSummary(t *testing.T) {
	cases := []struct {
		name        string
		gh          doctor.Status
		hf          doctor.Status
		mustContain []string
		mustNot     []string
	}{
		{
			name:        "all pass",
			gh:          doctor.StatusPass,
			hf:          doctor.StatusPass,
			mustContain: []string{"You're all set up!"},
			mustNot:     []string{"gh auth login", "hf auth login"},
		},
		{
			name:        "gh fail only",
			gh:          doctor.StatusFail,
			hf:          doctor.StatusPass,
			mustContain: []string{"gh auth login"},
			mustNot:     []string{"You're all set up", "hf auth login"},
		},
		{
			name:        "hf warn only",
			gh:          doctor.StatusPass,
			hf:          doctor.StatusWarn,
			mustContain: []string{"hf auth login"},
			mustNot:     []string{"You're all set up", "gh auth login"},
		},
		{
			name:        "both gh fail and hf warn",
			gh:          doctor.StatusFail,
			hf:          doctor.StatusWarn,
			mustContain: []string{"gh auth login", "hf auth login"},
			mustNot:     []string{"You're all set up"},
		},
		{
			name:        "hf fail",
			gh:          doctor.StatusPass,
			hf:          doctor.StatusFail,
			mustContain: []string{"hf auth login"},
			mustNot:     []string{"You're all set up", "gh auth login"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sections := []doctor.CheckSection{{
				Name: "Identity",
				Checks: []doctor.CheckResult{
					{Label: "GitHub CLI auth", Status: tc.gh},
					{Label: "Hugging Face auth", Status: tc.hf},
				},
			}}
			got := closingSummary(sections)
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("closingSummary missing %q in:\n%s", want, got)
				}
			}
			for _, bad := range tc.mustNot {
				if strings.Contains(got, bad) {
					t.Errorf("closingSummary should not contain %q in:\n%s", bad, got)
				}
			}
		})
	}
}
