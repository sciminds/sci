package cass

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiffLocal_NoPendingChanges(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestDiffRemote_DetectsConflicts(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	now := time.Now().Format(time.RFC3339)

	// Set up students.
	_ = db.UpsertStudents([]Student{
		{CanvasID: 1, Name: "Alice"},
		{CanvasID: 2, Name: "Bob"},
	})

	// Insert grades and synced baseline so DiffLocal finds changes.
	for _, q := range []string{
		`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
			VALUES (1, 'lab-1', 101, 201, '90', {:now})`,
		`INSERT INTO _grades_synced (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, synced_at)
			VALUES (1, 'lab-1', 101, 201, '80', {:now})`,
		`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
			VALUES (2, 'lab-1', 102, 201, '85', {:now})`,
		`INSERT INTO _grades_synced (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, synced_at)
			VALUES (2, 'lab-1', 102, 201, '80', {:now})`,
	} {
		if _, err := db.db.NewQuery(q).Bind(map[string]any{"now": now}).Execute(); err != nil {
			t.Fatal(err)
		}
	}

	// Mock Canvas submissions endpoint — returns live grades.
	// Alice's live grade matches baseline (no conflict), Bob's diverged (conflict).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		subs := []map[string]any{
			{"user_id": 101, "grade": "80", "workflow_state": "graded"}, // matches baseline
			{"user_id": 102, "grade": "75", "workflow_state": "graded"}, // diverged from baseline "80"
		}
		_ = json.NewEncoder(w).Encode(subs)
	}))
	defer srv.Close()

	result, err := DiffRemote(context.Background(), db, srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(result.Changes))
	}

	if result.Conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", result.Conflicts)
	}

	// Find Bob's change and verify it's the conflict.
	for _, c := range result.Changes {
		if c.CanvasUserID == 102 {
			if !c.Conflict {
				t.Error("expected Bob's change to be a conflict")
			}
			if c.Live != "75" {
				t.Errorf("live = %q, want %q", c.Live, "75")
			}
		}
		if c.CanvasUserID == 101 {
			if c.Conflict {
				t.Error("Alice's change should not be a conflict")
			}
		}
	}
}

func TestDiffRemote_NoChanges(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	result, err := DiffRemote(context.Background(), db, "http://unused", "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Errorf("expected no changes, got %d", len(result.Changes))
	}
}

func TestDiffLocal_NullBaseline(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	now := time.Now().Format(time.RFC3339)

	_ = db.UpsertStudents([]Student{{CanvasID: 1, Name: "Alice"}})

	// Grade with no synced baseline (new grade).
	_, _ = db.db.NewQuery(`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
		VALUES (1, 'lab-1', 1, 101, '95', {:now})`).Bind(map[string]any{"now": now}).Execute()

	result, err := DiffLocal(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(result.Changes))
	}
	// Baseline should be empty (NULL → "").
	if result.Changes[0].Baseline != "" {
		t.Errorf("baseline = %q, want empty for NULL", result.Changes[0].Baseline)
	}
	if result.Changes[0].StudentName != "Alice" {
		t.Errorf("name = %q", result.Changes[0].StudentName)
	}
}

func TestDiffResult_Human_NoChanges(t *testing.T) {
	t.Parallel()
	r := &DiffResult{}
	out := r.Human()
	if out == "" {
		t.Error("expected non-empty output for no changes")
	}
}

func TestRevert(t *testing.T) {
	t.Parallel()
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
