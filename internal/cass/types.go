package cass

import (
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// PullResult is the output of the pull command.
type PullResult struct {
	Changelogs []*Changelog `json:"changelogs"`
}

func (r *PullResult) JSON() any { return r }

func (r *PullResult) Human() string {
	var b strings.Builder
	for i, cl := range r.Changelogs {
		nl := "\n"
		if i == 0 {
			nl = ""
		}
		if cl.Added == 0 && cl.Updated == 0 {
			fmt.Fprintf(&b, "%s  %s %s — no changes\n", nl, ui.SymOK, ui.TUI.Bold().Render(cl.Entity))
			continue
		}
		fmt.Fprintf(&b, "%s  %s %s\n", nl, ui.SymOK, ui.TUI.Bold().Render(cl.Entity))
		for _, d := range cl.Details {
			fmt.Fprintf(&b, "    %s\n", d)
		}
		var parts []string
		if cl.Added > 0 {
			parts = append(parts, fmt.Sprintf("%d new", cl.Added))
		}
		if cl.Updated > 0 {
			parts = append(parts, fmt.Sprintf("%d updated", cl.Updated))
		}
		fmt.Fprintf(&b, "    %s\n", ui.TUI.Dim().Render(strings.Join(parts, ", ")))
	}
	return b.String()
}

// StatusResult is the output of the status command.
type StatusResult struct {
	CourseName      string        `json:"course_name,omitempty"`
	CanvasURL       string        `json:"canvas_url"`
	HasClassroom    bool          `json:"has_classroom"`
	LastPull        string        `json:"last_pull,omitempty"`
	StudentCount    int           `json:"student_count"`
	AssignmentCount int           `json:"assignment_count"`
	PendingGrades   int           `json:"pending_grades"`
	MatchPending    bool          `json:"match_pending"`
	UnmatchedCount  int           `json:"unmatched_count"`
	Discrepancies   []Discrepancy `json:"discrepancies,omitempty"`
}

// Discrepancy records a data mismatch between Canvas and GitHub.
type Discrepancy struct {
	Assignment string `json:"assignment"`
	Field      string `json:"field"`
	Canvas     string `json:"canvas"`
	GitHub     string `json:"github"`
}

func (r *StatusResult) JSON() any { return r }

func (r *StatusResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s\n", ui.TUI.Bold().Render("Course:"), r.CanvasURL)
	if r.HasClassroom {
		fmt.Fprintf(&b, "  %s GitHub Classroom configured\n", ui.SymOK)
	}
	if r.LastPull != "" {
		fmt.Fprintf(&b, "  %s Last pull: %s\n", ui.TUI.Dim().Render("  "), r.LastPull)
	} else {
		fmt.Fprintf(&b, "  %s %s\n", ui.SymWarn, "Never pulled — run 'sci cass pull'")
	}

	fmt.Fprintf(&b, "\n  %d students, %d assignments\n", r.StudentCount, r.AssignmentCount)

	if r.PendingGrades > 0 {
		fmt.Fprintf(&b, "  %s %d pending grade changes\n", ui.SymArrow, r.PendingGrades)
	}

	if r.MatchPending {
		fmt.Fprintf(&b, "  %s %d unmatched GitHub users — run 'sci cass match'\n", ui.SymWarn, r.UnmatchedCount)
	}

	for _, d := range r.Discrepancies {
		fmt.Fprintf(&b, "  %s %s: %s Canvas(%s) ≠ GitHub(%s)\n",
			ui.SymWarn, d.Assignment, d.Field, d.Canvas, d.GitHub)
	}

	return b.String()
}

// LogResult is the output of the log command.
type LogResult struct {
	Entries []LogEntry `json:"entries"`
}

func (r *LogResult) JSON() any { return r }

func (r *LogResult) Human() string {
	if len(r.Entries) == 0 {
		return "  No operations logged yet.\n"
	}
	var b strings.Builder
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "  %s  %-7s  %s\n",
			ui.TUI.Dim().Render(e.CreatedAt), e.Op, e.Summary)
	}
	return b.String()
}
