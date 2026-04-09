package cass

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sciminds/cli/internal/cass/api/canvas"
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

func TestPollProgress_Completed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 1, WorkflowState: "completed"})
	}))
	defer srv.Close()

	client := canvas.NewClient(srv.URL, "token")
	err := pollProgress(context.Background(), client, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPollProgress_Failed(t *testing.T) {
	msg := "grades could not be posted"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 1, WorkflowState: "failed", Message: &msg})
	}))
	defer srv.Close()

	client := canvas.NewClient(srv.URL, "token")
	err := pollProgress(context.Background(), client, 1)
	if err == nil {
		t.Fatal("expected error for failed progress")
	}
	if err.Error() != "canvas operation failed: grades could not be posted" {
		t.Errorf("error = %q", err)
	}
}

func TestPollProgress_FailedNoMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 1, WorkflowState: "failed"})
	}))
	defer srv.Close()

	client := canvas.NewClient(srv.URL, "token")
	err := pollProgress(context.Background(), client, 1)
	if err == nil {
		t.Fatal("expected error for failed progress")
	}
	if err.Error() != "canvas operation failed: unknown error" {
		t.Errorf("error = %q", err)
	}
}

func TestPollProgress_EventuallyCompletes(t *testing.T) {
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		state := "running"
		if callCount >= 3 {
			state = "completed"
		}
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 1, WorkflowState: state})
	}))
	defer srv.Close()

	client := canvas.NewClient(srv.URL, "token")
	// We can't easily speed up time.Sleep in pollProgress, but we can verify it completes.
	err := pollProgress(context.Background(), client, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount < 3 {
		t.Errorf("call count = %d, want >= 3", callCount)
	}
}

func TestPollProgress_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 1, WorkflowState: "running"})
	}))
	defer srv.Close()

	client := canvas.NewClient(srv.URL, "token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := pollProgress(ctx, client, 1)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestPushGrades_EndToEnd(t *testing.T) {
	db := openTestDB(t)

	// Mock Canvas: accept bulk grade POST, return progress, then complete.
	var gotForm bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			gotForm = true
			_ = r.ParseForm()
			if r.Form.Get("grade_data[101][posted_grade]") != "95" {
				t.Errorf("grade = %q", r.Form.Get("grade_data[101][posted_grade]"))
			}
			_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 42, WorkflowState: "queued"})
			return
		}
		// GET /progress/42
		_ = json.NewEncoder(w).Encode(canvas.Progress{ID: 42, WorkflowState: "completed"})
	}))
	defer srv.Close()

	changes := []GradeChange{
		{StudentID: 1, StudentName: "Alice", AssignmentSlug: "lab-1", CanvasUserID: 101, CanvasAssignmentID: 201, Current: "95"},
	}

	pushed, err := PushGrades(context.Background(), db, srv.URL, "token", 1, changes)
	if err != nil {
		t.Fatal(err)
	}
	if pushed != 1 {
		t.Errorf("pushed = %d, want 1", pushed)
	}
	if !gotForm {
		t.Error("expected grade POST to be sent")
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
