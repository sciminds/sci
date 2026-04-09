package cass

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestCreateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// All expected tables should exist.
	want := []string{"students", "assignments", "submissions", "grades", "_grades_synced", "log", "meta"}
	for _, table := range want {
		var count int
		err := db.db.NewQuery("SELECT count(*) FROM sqlite_master WHERE type='table' AND name={:name}").
			Bind(map[string]any{"name": table}).Row(&count)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %q not found", table)
		}
	}

	// Schema version should be set.
	ver, err := db.GetMeta("schema_version")
	if err != nil {
		t.Fatalf("get schema_version: %v", err)
	}
	if ver != schemaVersion {
		t.Errorf("schema_version = %q, want %q", ver, schemaVersion)
	}
}

func TestSchemaVersionMismatch_Recreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}

	// Insert some data.
	_ = db.UpsertStudents([]Student{{CanvasID: 1, Name: "Alice"}})

	// Tamper with schema version.
	_ = db.SetMeta("schema_version", "0")
	_ = db.Close()

	// Reopen — should detect mismatch and recreate.
	db2, err := OpenDB(path)
	if err != nil {
		t.Fatalf("reopen after version change: %v", err)
	}
	defer func() { _ = db2.Close() }()

	// Old data should be gone.
	students, _ := db2.AllStudents()
	if len(students) != 0 {
		t.Errorf("expected 0 students after schema reset, got %d", len(students))
	}

	// Schema version should be current.
	ver, _ := db2.GetMeta("schema_version")
	if ver != schemaVersion {
		t.Errorf("version = %q, want %q", ver, schemaVersion)
	}
}

func TestMetaGetSet(t *testing.T) {
	db := openTestDB(t)

	// Set and get.
	if err := db.SetMeta("foo", "bar"); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetMeta("foo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "bar" {
		t.Errorf("got %q, want %q", got, "bar")
	}

	// Update existing.
	if err := db.SetMeta("foo", "baz"); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetMeta("foo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "baz" {
		t.Errorf("got %q, want %q", got, "baz")
	}

	// Missing key.
	got, err = db.GetMeta("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q for missing key, want empty", got)
	}
}

func TestUpsertStudents(t *testing.T) {
	db := openTestDB(t)

	// Insert initial students.
	students := []Student{
		{CanvasID: 1, Name: "Alice", Email: "alice@test.com"},
		{CanvasID: 2, Name: "Bob", Email: "bob@test.com"},
	}
	if err := db.UpsertStudents(students); err != nil {
		t.Fatal(err)
	}

	// Verify count.
	got, err := db.AllStudents()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	// Set a local field (github_username) manually.
	_, err = db.db.NewQuery("UPDATE students SET github_username='alice-gh' WHERE canvas_id=1").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Re-upsert with updated remote fields — local fields must survive.
	students[0].Name = "Alice Updated"
	students[0].Email = "alice-new@test.com"
	if err := db.UpsertStudents(students); err != nil {
		t.Fatal(err)
	}

	got, err = db.AllStudents()
	if err != nil {
		t.Fatal(err)
	}

	var alice Student
	for _, s := range got {
		if s.CanvasID == 1 {
			alice = s
			break
		}
	}
	if alice.Name != "Alice Updated" {
		t.Errorf("name = %q, want %q", alice.Name, "Alice Updated")
	}
	if alice.GitHubUsername != "alice-gh" {
		t.Errorf("github_username = %q, want %q — local field not preserved", alice.GitHubUsername, "alice-gh")
	}
}

func TestUpsertAssignments(t *testing.T) {
	db := openTestDB(t)

	assignments := []AssignmentRow{
		{Slug: "lab-1", Title: "Lab 1", CanvasID: intPtr(101), PointsPossible: 20, Published: true},
		{Slug: "lab-2", Title: "Lab 2", CanvasID: intPtr(102), PointsPossible: 25},
	}
	if err := db.UpsertAssignments(assignments); err != nil {
		t.Fatal(err)
	}

	got, err := db.AllAssignments()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	// Update — remote fields change.
	assignments[0].PointsPossible = 30
	if err := db.UpsertAssignments(assignments); err != nil {
		t.Fatal(err)
	}

	got, err = db.AllAssignments()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.Slug == "lab-1" && a.PointsPossible != 30 {
			t.Errorf("points = %v, want 30", a.PointsPossible)
		}
	}
}

func TestReplaceSubmissions(t *testing.T) {
	db := openTestDB(t)

	subs := []SubmissionRow{
		{StudentID: 1, AssignmentSlug: "lab-1", Source: "canvas", Submitted: true, FetchedAt: "2026-04-08T00:00:00Z"},
		{StudentID: 2, AssignmentSlug: "lab-1", Source: "canvas", FetchedAt: "2026-04-08T00:00:00Z"},
	}
	if err := db.ReplaceSubmissions(subs); err != nil {
		t.Fatal(err)
	}

	// Replace with new data.
	subs2 := []SubmissionRow{
		{StudentID: 1, AssignmentSlug: "lab-1", Source: "canvas", Submitted: true, FetchedAt: "2026-04-08T01:00:00Z"},
	}
	if err := db.ReplaceSubmissions(subs2); err != nil {
		t.Fatal(err)
	}

	// Should have 1 row now (old data replaced).
	var count int
	if err := db.db.NewQuery("SELECT count(*) FROM submissions").Row(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestUpsertStudents_PreservesExcluded(t *testing.T) {
	db := openTestDB(t)

	// Insert a student and mark as excluded.
	_ = db.UpsertStudents([]Student{{CanvasID: 1, Name: "Alice", Email: "a@test.com"}})
	_, _ = db.db.NewQuery("UPDATE students SET excluded=1 WHERE canvas_id=1").Execute()

	// Re-upsert with updated name.
	_ = db.UpsertStudents([]Student{{CanvasID: 1, Name: "Alice Updated", Email: "a2@test.com"}})

	students, _ := db.AllStudents()
	if len(students) != 1 {
		t.Fatalf("len = %d", len(students))
	}
	if students[0].Name != "Alice Updated" {
		t.Errorf("name = %q", students[0].Name)
	}
	if !students[0].Excluded {
		t.Error("excluded flag should be preserved across upsert")
	}
}

func TestUpsertSubmissions_Batch(t *testing.T) {
	db := openTestDB(t)

	subs := []SubmissionRow{
		{StudentID: 1, AssignmentSlug: "lab-1", Source: "github", Submitted: true, FetchedAt: "2026-04-08T00:00:00Z"},
		{StudentID: 2, AssignmentSlug: "lab-1", Source: "github", Submitted: false, FetchedAt: "2026-04-08T00:00:00Z"},
	}
	if err := db.UpsertSubmissions(subs); err != nil {
		t.Fatal(err)
	}

	var count int
	_ = db.db.NewQuery("SELECT count(*) FROM submissions").Row(&count)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	// Update one submission via upsert.
	subs[0].Submitted = false
	if err := db.UpsertSubmissions(subs); err != nil {
		t.Fatal(err)
	}

	_ = db.db.NewQuery("SELECT count(*) FROM submissions").Row(&count)
	if count != 2 {
		t.Errorf("count after upsert = %d, want 2", count)
	}
}

func TestUpsertStudents_Empty(t *testing.T) {
	db := openTestDB(t)
	if err := db.UpsertStudents(nil); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertAssignments_Empty(t *testing.T) {
	db := openTestDB(t)
	if err := db.UpsertAssignments(nil); err != nil {
		t.Fatal(err)
	}
}

func TestWriteLog(t *testing.T) {
	db := openTestDB(t)

	if err := db.WriteLog("pull", "42 students, 8 assignments", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteLog("push", "5 grades → Lab 2", `{"assignment":"lab-2"}`); err != nil {
		t.Fatal(err)
	}

	entries, err := db.ReadLog(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	// Most recent first.
	if entries[0].Op != "push" {
		t.Errorf("first entry op = %q, want %q", entries[0].Op, "push")
	}
}

func TestReadLog_Limit(t *testing.T) {
	db := openTestDB(t)

	for i := range 5 {
		_ = db.WriteLog("op", fmt.Sprintf("entry %d", i), "")
	}

	entries, err := db.ReadLog(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("len = %d, want 3", len(entries))
	}
	// Most recent first.
	if entries[0].Summary != "entry 4" {
		t.Errorf("first = %q, want %q", entries[0].Summary, "entry 4")
	}
}

func TestReplaceSubmissions_Empty(t *testing.T) {
	db := openTestDB(t)

	// Insert some initial data.
	_ = db.ReplaceSubmissions([]SubmissionRow{
		{StudentID: 1, AssignmentSlug: "lab-1", Source: "canvas", FetchedAt: "2026-04-08T00:00:00Z"},
	})

	// Replace with empty list — should clear all.
	if err := db.ReplaceSubmissions(nil); err != nil {
		t.Fatal(err)
	}

	var count int
	_ = db.db.NewQuery("SELECT count(*) FROM submissions").Row(&count)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestSetStudentGitHubUsername(t *testing.T) {
	db := openTestDB(t)
	_ = db.UpsertStudents([]Student{{CanvasID: 1, Name: "Alice"}})

	if err := db.SetStudentGitHubUsername(1, "alice-gh"); err != nil {
		t.Fatal(err)
	}

	students, _ := db.AllStudents()
	if students[0].GitHubUsername != "alice-gh" {
		t.Errorf("username = %q", students[0].GitHubUsername)
	}

	// Setting on non-existent ID should not error (UPDATE affects 0 rows).
	if err := db.SetStudentGitHubUsername(999, "nobody"); err != nil {
		t.Fatalf("unexpected error for non-existent student: %v", err)
	}
}

// --- helpers ---

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func intPtr(v int) *int { return &v }
