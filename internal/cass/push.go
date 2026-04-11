package cass

import (
	"context"
	"fmt"
	"time"

	"github.com/sciminds/cli/internal/cass/api/canvas"
)

// CheckPushGates verifies preconditions for pushing grades.
func CheckPushGates(db *DB) error {
	// Must have pulled at least once.
	lastPull, _ := db.GetMeta("last_pull")
	if lastPull == "" {
		return fmt.Errorf("no data yet — run 'sci cass pull' first")
	}

	// Must not have unresolved matches.
	matchPending, _ := db.GetMeta("match_pending")
	if matchPending == "true" {
		return fmt.Errorf("unmatched GitHub students — run 'sci cass match' before pushing")
	}

	return nil
}

// PushGrades sends pending grade changes to Canvas.
// Returns the number of grades successfully pushed.
func PushGrades(ctx context.Context, db *DB, canvasBaseURL, token string, courseID int, changes []GradeChange) (int, error) {
	client := canvas.NewClient(canvasBaseURL, token)

	// Group changes by assignment.
	byAssignment := make(map[int]map[int]string) // assignment_id → {user_id → grade}
	for _, c := range changes {
		if byAssignment[c.CanvasAssignmentID] == nil {
			byAssignment[c.CanvasAssignmentID] = make(map[int]string)
		}
		byAssignment[c.CanvasAssignmentID][c.CanvasUserID] = c.Current
	}

	pushed := 0
	for assignmentID, grades := range byAssignment {
		form := canvas.BulkGradeForm(grades)
		path := fmt.Sprintf("/courses/%d/assignments/%d/submissions/update_grades", courseID, assignmentID)

		var progress canvas.Progress
		if err := client.PostForm(ctx, path, form, &progress); err != nil {
			return pushed, fmt.Errorf("push grades for assignment %d: %w", assignmentID, err)
		}

		// Poll progress until complete.
		if err := pollProgress(ctx, client, progress.ID); err != nil {
			return pushed, fmt.Errorf("grade push progress for assignment %d: %w", assignmentID, err)
		}

		pushed += len(grades)
	}

	return pushed, nil
}

// pollProgress waits for a Canvas async operation to complete.
// The 2-minute timeout applies per-assignment, not to the total push operation.
// pollProgressInterval is how long pollProgress waits between polls. Overridable
// from tests so we don't pay wall-clock seconds per iteration.
var pollProgressInterval = time.Second

func pollProgress(ctx context.Context, client *canvas.Client, progressID int) error {
	path := fmt.Sprintf("/progress/%d", progressID)
	timeout := time.After(2 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timed out waiting for progress %d", progressID)
		default:
		}

		var p canvas.Progress
		if err := client.Get(ctx, path, nil, &p); err != nil {
			return err
		}

		switch p.WorkflowState {
		case "completed":
			return nil
		case "failed":
			msg := "unknown error"
			if p.Message != nil {
				msg = *p.Message
			}
			return fmt.Errorf("canvas operation failed: %s", msg)
		}

		time.Sleep(pollProgressInterval)
	}
}

// SyncGrades copies current grades to the shadow table (_grades_synced).
// Call this after a successful push.
func SyncGrades(db *DB) error {
	_, err := db.db.NewQuery("DELETE FROM _grades_synced").Execute()
	if err != nil {
		return err
	}
	_, err = db.db.NewQuery(`
		INSERT INTO _grades_synced (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, synced_at)
		SELECT student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, datetime('now')
		FROM grades
		WHERE posted_grade != ''
	`).Execute()
	return err
}
