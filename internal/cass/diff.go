package cass

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sciminds/cli/internal/cass/api/canvas"
	"github.com/sciminds/cli/internal/ui"
)

// GradeChange represents a pending grade change.
type GradeChange struct {
	StudentID          int    `json:"student_id"`
	StudentName        string `json:"student_name"`
	AssignmentSlug     string `json:"assignment_slug"`
	CanvasUserID       int    `json:"canvas_user_id"`
	CanvasAssignmentID int    `json:"canvas_assignment_id"`
	Baseline           string `json:"baseline"`
	Current            string `json:"current"`
}

// DiffResult holds all pending grade changes.
type DiffResult struct {
	Changes []GradeChange `json:"changes"`
}

func (r *DiffResult) JSON() any { return r }

func (r *DiffResult) Human() string {
	if len(r.Changes) == 0 {
		return fmt.Sprintf("  %s No pending grade changes.\n", ui.SymOK)
	}

	byAssignment, order := groupBySlug(r.Changes)

	var b strings.Builder
	for _, slug := range order {
		changes := byAssignment[slug]
		fmt.Fprintf(&b, "\n  %s — %d grade change(s)\n", ui.TUI.Bold().Render(slug), len(changes))
		fmt.Fprintf(&b, "    %-25s %-10s → %s\n",
			ui.TUI.Dim().Render("Student"),
			ui.TUI.Dim().Render("Baseline"),
			ui.TUI.Dim().Render("Local"))
		for _, c := range changes {
			baseline := c.Baseline
			if baseline == "" {
				baseline = "-"
			}
			fmt.Fprintf(&b, "    %-25s %-10s → %s\n", c.StudentName, baseline, c.Current)
		}
	}
	return b.String()
}

// groupBySlug groups items by assignment slug, preserving insertion order.
func groupBySlug[T interface{ slug() string }](items []T) (map[string][]T, []string) {
	grouped := make(map[string][]T)
	var order []string
	for _, item := range items {
		s := item.slug()
		if _, seen := grouped[s]; !seen {
			order = append(order, s)
		}
		grouped[s] = append(grouped[s], item)
	}
	return grouped, order
}

func (c GradeChange) slug() string       { return c.AssignmentSlug }
func (c RemoteGradeChange) slug() string { return c.AssignmentSlug }

// DiffLocal computes pending grade changes by comparing grades vs _grades_synced.
func DiffLocal(db *DB) (*DiffResult, error) {
	// Build student name lookup.
	students, err := db.AllStudents()
	if err != nil {
		return nil, err
	}
	nameMap := make(map[int]string, len(students))
	for _, s := range students {
		nameMap[s.CanvasID] = s.Name
	}

	type gradeRow struct {
		StudentID          int            `db:"student_id"`
		AssignmentSlug     string         `db:"assignment_slug"`
		CanvasUserID       int            `db:"canvas_user_id"`
		CanvasAssignmentID int            `db:"canvas_assignment_id"`
		PostedGrade        string         `db:"posted_grade"`
		Baseline           sql.NullString `db:"baseline"`
	}

	var rows []gradeRow
	err = db.db.NewQuery(`
		SELECT g.student_id, g.assignment_slug, g.canvas_user_id, g.canvas_assignment_id, g.posted_grade,
			   s.posted_grade AS baseline
		FROM grades g
		LEFT JOIN _grades_synced s ON g.student_id = s.student_id AND g.assignment_slug = s.assignment_slug
		WHERE g.posted_grade != '' AND (s.posted_grade IS NULL OR g.posted_grade != s.posted_grade)
		ORDER BY g.assignment_slug, g.student_id
	`).All(&rows)
	if err != nil {
		return nil, err
	}

	changes := make([]GradeChange, len(rows))
	for i, r := range rows {
		baseline := ""
		if r.Baseline.Valid {
			baseline = r.Baseline.String
		}
		changes[i] = GradeChange{
			StudentID:          r.StudentID,
			StudentName:        nameMap[r.StudentID],
			AssignmentSlug:     r.AssignmentSlug,
			CanvasUserID:       r.CanvasUserID,
			CanvasAssignmentID: r.CanvasAssignmentID,
			Baseline:           baseline,
			Current:            r.PostedGrade,
		}
	}

	return &DiffResult{Changes: changes}, nil
}

// RemoteGradeChange extends GradeChange with the live Canvas score.
type RemoteGradeChange struct {
	GradeChange
	Live     string `json:"live"`
	Conflict bool   `json:"conflict"`
}

// RemoteDiffResult holds pending grade changes with live Canvas comparison.
type RemoteDiffResult struct {
	Changes   []RemoteGradeChange `json:"changes"`
	Conflicts int                 `json:"conflicts"`
}

func (r *RemoteDiffResult) JSON() any { return r }

func (r *RemoteDiffResult) Human() string {
	if len(r.Changes) == 0 {
		return fmt.Sprintf("  %s No pending grade changes.\n", ui.SymOK)
	}

	byAssignment, order := groupBySlug(r.Changes)

	var b strings.Builder
	for _, slug := range order {
		changes := byAssignment[slug]
		conflicts := 0
		for _, c := range changes {
			if c.Conflict {
				conflicts++
			}
		}
		label := fmt.Sprintf("%d grade change(s)", len(changes))
		if conflicts > 0 {
			label += fmt.Sprintf(" (%d CONFLICT)", conflicts)
		}
		fmt.Fprintf(&b, "\n  %s — %s\n", ui.TUI.Bold().Render(slug), label)
		fmt.Fprintf(&b, "    %-25s %-10s %-10s → %s\n",
			ui.TUI.Dim().Render("Student"),
			ui.TUI.Dim().Render("Baseline"),
			ui.TUI.Dim().Render("Canvas"),
			ui.TUI.Dim().Render("Local"))
		for _, c := range changes {
			baseline := c.Baseline
			if baseline == "" {
				baseline = "-"
			}
			live := c.Live
			if live == "" {
				live = "-"
			}
			marker := " "
			if c.Conflict {
				marker = ui.SymWarn
			}
			fmt.Fprintf(&b, "  %s %-25s %-10s %-10s → %s\n", marker, c.StudentName, baseline, live, c.Current)
		}
	}
	if r.Conflicts > 0 {
		fmt.Fprintf(&b, "\n  %s %d conflict(s) — Canvas was edited since last pull\n", ui.SymWarn, r.Conflicts)
	}
	return b.String()
}

// DiffRemote computes pending grade changes with a 3-way comparison against live Canvas scores.
func DiffRemote(ctx context.Context, db *DB, canvasBaseURL, token string, courseID int) (*RemoteDiffResult, error) {
	local, err := DiffLocal(db)
	if err != nil {
		return nil, err
	}
	if len(local.Changes) == 0 {
		return &RemoteDiffResult{}, nil
	}

	client := canvas.NewClient(canvasBaseURL, token)

	// Group changes by assignment to batch fetches.
	byAssignment := make(map[int][]GradeChange)
	for _, c := range local.Changes {
		byAssignment[c.CanvasAssignmentID] = append(byAssignment[c.CanvasAssignmentID], c)
	}

	result := RemoteDiffResult{
		Changes: make([]RemoteGradeChange, 0, len(local.Changes)),
	}
	for assignmentID, changes := range byAssignment {
		// Fetch live submissions for this assignment.
		path := fmt.Sprintf("/courses/%d/assignments/%d/submissions", courseID, assignmentID)
		var subs []canvas.Submission
		if err := client.GetPaginated(ctx, path, nil, &subs); err != nil {
			return nil, fmt.Errorf("fetch live submissions for assignment %d: %w", assignmentID, err)
		}

		// Build lookup: user_id → live grade/score.
		liveGrades := make(map[int]string, len(subs))
		for _, s := range subs {
			if s.Grade != nil {
				liveGrades[s.UserID] = *s.Grade
			}
		}

		for _, c := range changes {
			live := liveGrades[c.CanvasUserID]
			conflict := live != "" && c.Baseline != live
			rc := RemoteGradeChange{
				GradeChange: c,
				Live:        live,
				Conflict:    conflict,
			}
			if conflict {
				result.Conflicts++
			}
			result.Changes = append(result.Changes, rc)
		}
	}

	return &result, nil
}

// Revert discards all pending grade edits by copying _grades_synced back to grades.
// Returns the number of grades reverted.
func Revert(db *DB) (int, error) {
	// Delete grades that have no synced baseline.
	_, err := db.db.NewQuery(`
		DELETE FROM grades WHERE NOT EXISTS (
			SELECT 1 FROM _grades_synced s
			WHERE s.student_id = grades.student_id AND s.assignment_slug = grades.assignment_slug
		)
	`).Execute()
	if err != nil {
		return 0, err
	}

	// Restore from baseline.
	res, err := db.db.NewQuery(`
		UPDATE grades SET posted_grade = (
			SELECT s.posted_grade FROM _grades_synced s
			WHERE s.student_id = grades.student_id AND s.assignment_slug = grades.assignment_slug
		), updated_at = datetime('now')
		WHERE EXISTS (
			SELECT 1 FROM _grades_synced s
			WHERE s.student_id = grades.student_id AND s.assignment_slug = grades.assignment_slug
			AND s.posted_grade != grades.posted_grade
		)
	`).Execute()
	if err != nil {
		return 0, err
	}

	n, _ := res.RowsAffected()
	return int(n), nil
}
