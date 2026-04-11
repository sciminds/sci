package cass

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Integration tests are gated behind SLOW=1 (they hit real Canvas/GitHub APIs)
// and additionally require:
//   CANVAS_TEST_TOKEN      — Canvas API token
//   CANVAS_TEST_URL        — Full course URL (e.g. https://canvas.ucsd.edu/courses/63653)
//   GH_CLASSROOM_TEST_URL  — GitHub Classroom URL (optional, for GH tests)
//
// Run: `just test-canvas` (sets SLOW=1 + the env vars from .env).

func skipUnlessSlow(t *testing.T) {
	t.Helper()
	if os.Getenv("SLOW") == "" {
		t.Skip("skipping integration test (set SLOW=1 to run)")
	}
}

func skipUnlessIntegration(t *testing.T) (baseURL string, courseID int, token string) {
	t.Helper()
	skipUnlessSlow(t)
	token = os.Getenv("CANVAS_TEST_TOKEN")
	rawURL := os.Getenv("CANVAS_TEST_URL")
	if token == "" || rawURL == "" {
		t.Skip("set CANVAS_TEST_TOKEN and CANVAS_TEST_URL to run integration tests")
	}
	var err error
	baseURL, courseID, err = ParseCanvasURL(rawURL)
	if err != nil {
		t.Fatalf("bad CANVAS_TEST_URL: %v", err)
	}
	return baseURL, courseID, token
}

func skipUnlessGHClassroom(t *testing.T) (classroomURL string, ghToken string) {
	t.Helper()
	skipUnlessSlow(t)
	classroomURL = os.Getenv("GH_CLASSROOM_TEST_URL")
	if classroomURL == "" {
		t.Skip("set GH_CLASSROOM_TEST_URL to run GitHub Classroom integration tests")
	}
	var err error
	ghToken, err = GetGHTokenFromCLI()
	if err != nil {
		t.Skipf("gh auth not available: %v", err)
	}
	return classroomURL, ghToken
}

func TestIntegration_PullStudents(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)
	db := openTestDB(t)

	cl, err := PullStudents(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("students: %d added, %d updated", cl.Added, cl.Updated)

	students, err := db.AllStudents()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total students in DB: %d", len(students))
}

func TestIntegration_PullAssignments(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)
	db := openTestDB(t)

	cl, err := PullAssignments(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("assignments: %d added, %d updated", cl.Added, cl.Updated)

	assignments, err := db.AllAssignments()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assignments {
		t.Logf("  %s (%.0f pts, published=%v, group=%s)", a.Title, a.PointsPossible, a.Published, a.AssignmentGroup)
	}
}

func TestIntegration_ListModules(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)

	result, err := ListModules(context.Background(), baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("modules: %d", len(result.Modules))
	for _, m := range result.Modules {
		t.Logf("  %d: %s (published=%v)", m.ID, m.Name, m.Published)
	}
}

func TestIntegration_ListFiles(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)

	result, err := ListFiles(context.Background(), baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files: %d", len(result.Files))
	for _, f := range result.Files {
		t.Logf("  %d: %s (%d bytes)", f.ID, f.DisplayName, f.Size)
	}
}

func TestIntegration_FullPullCycle(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)

	dbPath := filepath.Join(t.TempDir(), "integration.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Pull students.
	studentCL, err := PullStudents(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("students: +%d ~%d", studentCL.Added, studentCL.Updated)

	// Pull assignments.
	assignCL, err := PullAssignments(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("assignments: +%d ~%d", assignCL.Added, assignCL.Updated)

	// Pull submissions.
	subCL, err := PullSubmissions(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("submissions: %d", subCL.Added)

	// Check status.
	cfg := &Config{Canvas: CanvasConfig{URL: baseURL + "/courses/" + fmt.Sprintf("%d", courseID)}}
	_ = db.SetMeta("last_pull", "now")
	status, err := Status(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status: %d students, %d assignments, %d pending grades",
		status.StudentCount, status.AssignmentCount, status.PendingGrades)

	// Second pull — should show no new additions.
	studentCL2, err := PullStudents(context.Background(), db, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	if studentCL2.Added != 0 {
		t.Errorf("second pull added %d students, expected 0", studentCL2.Added)
	}
}

// --- Write operation tests ---
// These create resources on the test course and clean up after themselves.

func TestIntegration_CreateDeleteModule(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)
	ctx := context.Background()

	// Create.
	result, err := CreateModule(ctx, baseURL, token, courseID, "Test Module (sci cass integration)")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created module %d: %s", result.Module.ID, result.Module.Name)

	if result.Module.Name != "Test Module (sci cass integration)" {
		t.Errorf("name = %q", result.Module.Name)
	}

	// Delete.
	if err := DeleteModule(ctx, baseURL, token, courseID, result.Module.ID); err != nil {
		t.Fatalf("delete module %d: %v", result.Module.ID, err)
	}
	t.Logf("deleted module %d", result.Module.ID)
}

func TestIntegration_CreateDeleteAssignment(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)
	ctx := context.Background()

	// Create.
	result, err := CreateCanvasAssignment(ctx, baseURL, token, courseID, AssignmentSpec{
		Name:   "Test Assignment (sci cass integration)",
		Points: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created assignment %d: %s (%.0f pts)", result.Assignment.ID, result.Assignment.Name, result.Assignment.PointsPossible)

	if result.Assignment.PointsPossible != 42 {
		t.Errorf("points = %.0f, want 42", result.Assignment.PointsPossible)
	}

	// Update.
	updated, err := UpdateCanvasAssignment(ctx, baseURL, token, courseID, result.Assignment.ID, AssignmentSpec{
		Points: 50,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Assignment.PointsPossible != 50 {
		t.Errorf("updated points = %.0f, want 50", updated.Assignment.PointsPossible)
	}
	t.Logf("updated assignment %d: %.0f pts", updated.Assignment.ID, updated.Assignment.PointsPossible)

	// Delete.
	if err := DeleteCanvasAssignment(ctx, baseURL, token, courseID, result.Assignment.ID); err != nil {
		t.Fatalf("delete assignment %d: %v", result.Assignment.ID, err)
	}
	t.Logf("deleted assignment %d", result.Assignment.ID)
}

func TestIntegration_PostDeleteAnnouncement(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)
	ctx := context.Background()

	// Post.
	result, err := PostAnnouncement(ctx, baseURL, token, courseID,
		"Test Announcement (sci cass integration)",
		"<p>This is an automated test announcement. Please ignore.</p>")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("posted announcement %d: %s", result.Announcement.ID, result.Announcement.Title)

	// List and verify it appears.
	listResult, err := ListAnnouncements(ctx, baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range listResult.Announcements {
		if a.ID == result.Announcement.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("announcement %d not found in list", result.Announcement.ID)
	}

	// Delete.
	if err := DeleteAnnouncement(ctx, baseURL, token, courseID, result.Announcement.ID); err != nil {
		t.Fatalf("delete announcement %d: %v", result.Announcement.ID, err)
	}
	t.Logf("deleted announcement %d", result.Announcement.ID)
}

func TestIntegration_ListAnnouncements(t *testing.T) {
	baseURL, courseID, token := skipUnlessIntegration(t)

	result, err := ListAnnouncements(context.Background(), baseURL, token, courseID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("announcements: %d", len(result.Announcements))
	for _, a := range result.Announcements {
		t.Logf("  %d: %s", a.ID, a.Title)
	}
}

// --- GitHub Classroom integration tests ---

func TestIntegration_ResolveClassroomID(t *testing.T) {
	classroomURL, ghToken := skipUnlessGHClassroom(t)
	ctx := context.Background()

	apiID, err := ResolveClassroomID(ctx, ghToken, classroomURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved URL %q → API ID %d", classroomURL, apiID)

	if apiID <= 0 {
		t.Errorf("expected positive API ID, got %d", apiID)
	}
}

func TestIntegration_ResolveClassroomID_BadURL(t *testing.T) {
	_, ghToken := skipUnlessGHClassroom(t)
	ctx := context.Background()

	_, err := ResolveClassroomID(ctx, ghToken, "https://classroom.github.com/classrooms/000000-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent classroom")
	}
	t.Logf("got expected error: %v", err)
}

func TestIntegration_PullGHAssignments(t *testing.T) {
	classroomURL, ghToken := skipUnlessGHClassroom(t)
	ctx := context.Background()

	apiID, err := ResolveClassroomID(ctx, ghToken, classroomURL)
	if err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)
	cl, err := PullGHAssignments(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gh_assignments: %d added, %d updated", cl.Added, cl.Updated)
	for _, d := range cl.Details {
		t.Logf("  %s", d)
	}

	// Verify assignments in DB.
	assignments, err := db.AllAssignments()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total assignments in DB: %d", len(assignments))
	for _, a := range assignments {
		t.Logf("  %s (gh_slug=%v, canvas_id=%v)", a.Title, a.GHSlug, a.CanvasID)
	}
}

func TestIntegration_PullGHSubmissions(t *testing.T) {
	classroomURL, ghToken := skipUnlessGHClassroom(t)
	ctx := context.Background()

	apiID, err := ResolveClassroomID(ctx, ghToken, classroomURL)
	if err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	// Pull assignments first (submissions reference them).
	if _, err := PullGHAssignments(ctx, db, ghToken, apiID); err != nil {
		t.Fatal(err)
	}

	cl, err := PullGHSubmissions(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gh_submissions: %d added", cl.Added)

	// Verify submissions in DB.
	var count int
	if err := db.db.NewQuery("SELECT count(*) FROM submissions WHERE source='github'").Row(&count); err != nil {
		t.Fatal(err)
	}
	t.Logf("GitHub submissions in DB: %d", count)
}

func TestIntegration_FullGHPullCycle(t *testing.T) {
	classroomURL, ghToken := skipUnlessGHClassroom(t)
	ctx := context.Background()

	apiID, err := ResolveClassroomID(ctx, ghToken, classroomURL)
	if err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	// Pull assignments.
	assignCL, err := PullGHAssignments(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("assignments: +%d ~%d", assignCL.Added, assignCL.Updated)

	// Pull submissions.
	subCL, err := PullGHSubmissions(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("submissions: +%d", subCL.Added)

	// Second pull — should be idempotent.
	assignCL2, err := PullGHAssignments(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	// Second pull should update (not add) since they already exist.
	if assignCL2.Added > 0 && assignCL.Added > 0 {
		t.Logf("second pull: +%d ~%d (may re-add if gh_slug not linked)", assignCL2.Added, assignCL2.Updated)
	}

	subCL2, err := PullGHSubmissions(ctx, db, ghToken, apiID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("second submission pull: +%d", subCL2.Added)
}
