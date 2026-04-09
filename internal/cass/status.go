package cass

import "fmt"

// Status computes the current sync status from the database.
func Status(db *DB, cfg *Config) (*StatusResult, error) {
	students, err := db.AllStudents()
	if err != nil {
		return nil, err
	}

	assignments, err := db.AllAssignments()
	if err != nil {
		return nil, err
	}

	lastPull, _ := db.GetMeta("last_pull")
	matchPending, _ := db.GetMeta("match_pending")

	// Count students with no GitHub username when classroom is configured.
	unmatchedCount := 0
	if cfg.HasClassroom() {
		var cnt int
		if err := db.db.NewQuery("SELECT count(*) FROM students WHERE github_username IS NULL OR github_username = ''").Row(&cnt); err == nil {
			unmatchedCount = cnt
		}
	}

	// Count pending grade changes.
	pendingGrades := 0
	var count int
	err = db.db.NewQuery(`
		SELECT count(*) FROM grades g
		LEFT JOIN _grades_synced s ON g.student_id = s.student_id AND g.assignment_slug = s.assignment_slug
		WHERE g.posted_grade != '' AND (s.posted_grade IS NULL OR g.posted_grade != s.posted_grade)
	`).Row(&count)
	if err == nil {
		pendingGrades = count
	}

	// Detect discrepancies between Canvas and GitHub assignment fields.
	var discrepancies []Discrepancy
	for _, a := range assignments {
		if !a.GHSlug.Valid {
			continue
		}
		if a.GHPoints != nil && a.PointsPossible != *a.GHPoints {
			discrepancies = append(discrepancies, Discrepancy{
				Assignment: a.Title,
				Field:      "points_possible",
				Canvas:     formatFloat(a.PointsPossible),
				GitHub:     formatFloat(*a.GHPoints),
			})
		}
		if a.GHDeadline.Valid && a.Deadline.Valid && a.Deadline.String != a.GHDeadline.String {
			discrepancies = append(discrepancies, Discrepancy{
				Assignment: a.Title,
				Field:      "deadline",
				Canvas:     a.Deadline.String,
				GitHub:     a.GHDeadline.String,
			})
		}
	}

	return &StatusResult{
		CanvasURL:       cfg.Canvas.URL,
		HasClassroom:    cfg.HasClassroom(),
		LastPull:        lastPull,
		StudentCount:    len(students),
		AssignmentCount: len(assignments),
		PendingGrades:   pendingGrades,
		MatchPending:    matchPending == "true",
		UnmatchedCount:  unmatchedCount,
		Discrepancies:   discrepancies,
	}, nil
}

func formatFloat(f float64) string {
	if f == float64(int(f)) {
		return fmt.Sprintf("%d", int(f))
	}
	return fmt.Sprintf("%.1f", f)
}
