package cass

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/cass/api/github"
)

func TestPullStudents_Changelog(t *testing.T) {
	t.Parallel()
	// Mock Canvas API returning 3 students.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		users := []map[string]any{
			{"id": 1, "name": "Alice Chen", "sortable_name": "Chen, Alice", "email": "alice@test.com", "login_id": "alice"},
			{"id": 2, "name": "Bob Park", "sortable_name": "Park, Bob", "email": "bob@test.com", "login_id": "bob"},
			{"id": 3, "name": "Carol Davis", "sortable_name": "Davis, Carol", "email": "carol@test.com", "login_id": "carol"},
		}
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer srv.Close()

	db := openTestDB(t)

	// First pull — all new.
	changelog, err := PullStudents(context.Background(), db, srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if changelog.Added != 3 {
		t.Errorf("added = %d, want 3", changelog.Added)
	}

	// Second pull — no changes.
	changelog, err = PullStudents(context.Background(), db, srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if changelog.Added != 0 || changelog.Updated != 0 {
		t.Errorf("expected no changes, got added=%d updated=%d", changelog.Added, changelog.Updated)
	}
}

func TestPullStudents_PreservesLocalFields(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		users := []map[string]any{
			{"id": 1, "name": "Alice Chen", "sortable_name": "Chen, Alice", "email": "alice@test.com", "login_id": "alice"},
		}
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer srv.Close()

	db := openTestDB(t)

	// Initial pull.
	if _, err := PullStudents(context.Background(), db, srv.URL, "token", 1); err != nil {
		t.Fatal(err)
	}

	// Set local field.
	_, err := db.db.NewQuery("UPDATE students SET github_username='alice-gh' WHERE canvas_id=1").Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Re-pull.
	if _, err := PullStudents(context.Background(), db, srv.URL, "token", 1); err != nil {
		t.Fatal(err)
	}

	students, err := db.AllStudents()
	if err != nil {
		t.Fatal(err)
	}
	if students[0].GitHubUsername != "alice-gh" {
		t.Errorf("github_username = %q, want %q", students[0].GitHubUsername, "alice-gh")
	}
}

func TestResolveClassroomID_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		classrooms := []map[string]any{
			{"id": 1, "name": "Other Course", "url": "https://classroom.github.com/classrooms/111-other"},
		}
		_ = json.NewEncoder(w).Encode(classrooms)
	}))
	defer srv.Close()

	client := github.NewClient("token")
	client.BaseURL = srv.URL

	_, err := resolveClassroomID(context.Background(), client, "https://classroom.github.com/classrooms/999-missing")
	if err == nil {
		t.Fatal("expected error for unmatched classroom URL")
	}
	if !strings.Contains(err.Error(), "Other Course") {
		t.Errorf("error should list available classrooms, got: %v", err)
	}
}

func TestResolveClassroomID_EmptyList(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	client := github.NewClient("token")
	client.BaseURL = srv.URL

	_, err := resolveClassroomID(context.Background(), client, "https://classroom.github.com/classrooms/999")
	if err == nil {
		t.Fatal("expected error for empty classroom list")
	}
	if !strings.Contains(err.Error(), "no classrooms found") {
		t.Errorf("error should mention no classrooms found, got: %v", err)
	}
}

func TestResolveClassroomID_Found(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		classrooms := []map[string]any{
			{"id": 42, "name": "My Course", "url": "https://classroom.github.com/classrooms/999-my-course"},
		}
		_ = json.NewEncoder(w).Encode(classrooms)
	}))
	defer srv.Close()

	client := github.NewClient("token")
	client.BaseURL = srv.URL

	id, err := resolveClassroomID(context.Background(), client, "https://classroom.github.com/classrooms/999-my-course")
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func TestPullAssignments_Changelog(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.URL.Path == "/api/v1/courses/1/assignment_groups" {
			groups := []map[string]any{
				{"id": 10, "name": "Labs"},
			}
			_ = json.NewEncoder(w).Encode(groups)
			return
		}
		assignments := []map[string]any{
			{"id": 101, "name": "Lab 1", "points_possible": 20.0, "due_at": "2026-04-10T23:59:00Z", "published": true, "assignment_group_id": 10, "post_manually": false, "workflow_state": "published"},
			{"id": 102, "name": "Lab 2", "points_possible": 25.0, "published": false, "assignment_group_id": 10, "post_manually": true, "workflow_state": "unpublished"},
		}
		_ = json.NewEncoder(w).Encode(assignments)
	}))
	defer srv.Close()

	db := openTestDB(t)

	changelog, err := PullAssignments(context.Background(), db, srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if changelog.Added != 2 {
		t.Errorf("added = %d, want 2", changelog.Added)
	}

	// Verify data.
	assignments, err := db.AllAssignments()
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 2 {
		t.Fatalf("len = %d, want 2", len(assignments))
	}
	if assignments[0].AssignmentGroup != "Labs" {
		t.Errorf("group = %q, want %q", assignments[0].AssignmentGroup, "Labs")
	}
}
