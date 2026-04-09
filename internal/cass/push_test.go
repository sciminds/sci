package cass

import (
	"testing"
	"time"
)

func TestPushGate_BlocksWhenMatchPending(t *testing.T) {
	db := openTestDB(t)
	_ = db.SetMeta("match_pending", "true")

	err := CheckPushGates(db)
	if err == nil {
		t.Fatal("expected push to be blocked when match_pending")
	}
}

func TestPushGate_BlocksWhenNeverPulled(t *testing.T) {
	db := openTestDB(t)

	err := CheckPushGates(db)
	if err == nil {
		t.Fatal("expected push to be blocked when never pulled")
	}
}

func TestPushGate_AllowsWhenClean(t *testing.T) {
	db := openTestDB(t)
	_ = db.SetMeta("last_pull", time.Now().Format(time.RFC3339))

	err := CheckPushGates(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncGrades(t *testing.T) {
	db := openTestDB(t)

	now := time.Now().Format(time.RFC3339)

	// Insert grades that were "pushed."
	_, err := db.db.NewQuery(`INSERT INTO grades (student_id, assignment_slug, canvas_user_id, canvas_assignment_id, posted_grade, updated_at)
		VALUES (1, 'lab-1', 1, 101, '18', {:now})`).Bind(map[string]any{"now": now}).Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Sync grades to shadow table.
	if err := SyncGrades(db); err != nil {
		t.Fatal(err)
	}

	// Verify shadow table has the grade.
	var grade string
	err = db.db.NewQuery("SELECT posted_grade FROM _grades_synced WHERE student_id=1").Row(&grade)
	if err != nil {
		t.Fatal(err)
	}
	if grade != "18" {
		t.Errorf("synced grade = %q, want %q", grade, "18")
	}

	// Now diff should show no changes.
	result, err := DiffLocal(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes after sync, got %d", len(result.Changes))
	}
}
