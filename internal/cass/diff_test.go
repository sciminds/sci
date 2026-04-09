package cass

import (
	"testing"
	"time"
)

func TestDiffLocal_NoPendingChanges(t *testing.T) {
	db := openTestDB(t)
	result, err := DiffLocal(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Errorf("expected no changes, got %d", len(result.Changes))
	}
}

func TestDiffLocal_DetectsChanges(t *testing.T) {
	db := openTestDB(t)

	now := time.Now().Format(time.RFC3339)

	// Insert a grade and its synced baseline.
	_, err := db.db.NewQuery(`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
		VALUES (1, 'lab-1', 1, 101, '18', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.NewQuery(`INSERT INTO _grades_synced (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, synced_at)
		VALUES (1, 'lab-1', 1, 101, '', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Add a second grade with no baseline (new entry).
	_, err = db.db.NewQuery(`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
		VALUES (2, 'lab-1', 2, 101, '15', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Add a student name so human output works.
	_ = db.UpsertStudents([]Student{
		{CanvasID: 1, Name: "Alice"},
		{CanvasID: 2, Name: "Bob"},
	})

	result, err := DiffLocal(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(result.Changes))
	}
}

func TestRevert(t *testing.T) {
	db := openTestDB(t)

	now := time.Now().Format(time.RFC3339)

	// Insert synced baseline.
	_, err := db.db.NewQuery(`INSERT INTO _grades_synced (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, synced_at)
		VALUES (1, 'lab-1', 1, 101, '10', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Insert modified grade.
	_, err = db.db.NewQuery(`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
		VALUES (1, 'lab-1', 1, 101, '20', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Revert.
	count, err := Revert(db)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("reverted = %d, want 1", count)
	}

	// Verify grade was reverted to baseline.
	var grade string
	err = db.db.NewQuery("SELECT posted_grade FROM grades WHERE student_id=1 AND assignment_slug='lab-1'").Row(&grade)
	if err != nil {
		t.Fatal(err)
	}
	if grade != "10" {
		t.Errorf("grade = %q, want %q", grade, "10")
	}
}
