package cass

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cass/api/canvas"
	"github.com/sciminds/cli/internal/cass/api/github"
	"golang.org/x/sync/errgroup"
)

// Submission source identifiers.
const (
	sourceCanvas = "canvas"
	sourceGitHub = "github"
)

// Changelog tracks what changed during a pull operation.
type Changelog struct {
	Entity  string   `json:"entity"`
	Added   int      `json:"added"`
	Updated int      `json:"updated"`
	Details []string `json:"details,omitempty"`
}

// PullStudents fetches Canvas students for a specific course and upserts them into the DB.
func PullStudents(ctx context.Context, db *DB, canvasBaseURL, token string, courseID int) (*Changelog, error) {
	client := canvas.NewClient(canvasBaseURL, token)

	existing, err := db.AllStudents()
	if err != nil {
		return nil, err
	}
	existingMap := lo.KeyBy(existing, func(s Student) int {
		return s.CanvasID
	})

	params := url.Values{
		"enrollment_type[]": {"student"},
		"include[]":         {"email"},
	}
	path := fmt.Sprintf("/courses/%d/users", courseID)

	var canvasUsers []canvas.User
	if err := client.GetPaginated(ctx, path, params, &canvasUsers); err != nil {
		return nil, fmt.Errorf("fetch students: %w", err)
	}

	return pullStudentsFromData(db, canvasUsers, existingMap)
}

func pullStudentsFromData(db *DB, canvasUsers []canvas.User, existingMap map[int]Student) (*Changelog, error) {
	students := lo.Map(canvasUsers, func(u canvas.User, _ int) Student {
		return Student{
			CanvasID:     u.ID,
			Name:         u.Name,
			SortableName: u.SortableName,
			Email:        u.Email,
			LoginID:      u.LoginID,
		}
	})

	cl := &Changelog{Entity: "students"}
	for _, s := range students {
		old, exists := existingMap[s.CanvasID]
		if !exists {
			cl.Added++
			cl.Details = append(cl.Details, fmt.Sprintf("+ %s", s.Name))
		} else if old.Name != s.Name || old.Email != s.Email {
			cl.Updated++
			cl.Details = append(cl.Details, fmt.Sprintf("~ %s", s.Name))
		}
	}

	if err := db.UpsertStudents(students); err != nil {
		return nil, err
	}
	return cl, nil
}

// PullAssignments fetches Canvas assignments and upserts them into the DB.
func PullAssignments(ctx context.Context, db *DB, canvasBaseURL, token string, courseID int) (*Changelog, error) {
	client := canvas.NewClient(canvasBaseURL, token)

	// Fetch assignment groups for name mapping.
	var groups []canvas.AssignmentGroup
	groupPath := fmt.Sprintf("/courses/%d/assignment_groups", courseID)
	if err := client.GetPaginated(ctx, groupPath, nil, &groups); err != nil {
		return nil, fmt.Errorf("fetch assignment groups: %w", err)
	}
	groupNames := lo.SliceToMap(groups, func(g canvas.AssignmentGroup) (int, string) {
		return g.ID, g.Name
	})

	// Fetch assignments.
	var canvasAssignments []canvas.Assignment
	assignPath := fmt.Sprintf("/courses/%d/assignments", courseID)
	if err := client.GetPaginated(ctx, assignPath, nil, &canvasAssignments); err != nil {
		return nil, fmt.Errorf("fetch assignments: %w", err)
	}

	// Get existing for change detection.
	existing, err := db.AllAssignments()
	if err != nil {
		return nil, err
	}
	existingMap := lo.KeyBy(existing, func(a AssignmentRow) string {
		return a.Slug
	})

	// Convert to domain type.
	rows := make([]AssignmentRow, len(canvasAssignments))
	for i, a := range canvasAssignments {
		slug := Slugify(a.Name)
		cid := a.ID
		rows[i] = AssignmentRow{
			Slug:            slug,
			Title:           a.Name,
			CanvasID:        &cid,
			PointsPossible:  a.PointsPossible,
			Published:       a.Published,
			AssignmentGroup: groupNames[a.AssignmentGroupID],
			PostManually:    a.PostManually,
		}
		if a.DueAt != nil {
			rows[i].Deadline = sql.NullString{String: *a.DueAt, Valid: true}
		}
	}

	// Compute changelog.
	cl := &Changelog{Entity: "assignments"}
	for _, r := range rows {
		old, exists := existingMap[r.Slug]
		if !exists {
			cl.Added++
			cl.Details = append(cl.Details, fmt.Sprintf("+ %s", r.Title))
		} else if old.PointsPossible != r.PointsPossible || old.Published != r.Published {
			cl.Updated++
			cl.Details = append(cl.Details, fmt.Sprintf("~ %s", r.Title))
		}
	}

	if err := db.UpsertAssignments(rows); err != nil {
		return nil, err
	}

	return cl, nil
}

// PullSubmissions fetches Canvas submissions for all assignments concurrently.
func PullSubmissions(ctx context.Context, db *DB, canvasBaseURL, token string, courseID int) (*Changelog, error) {
	client := canvas.NewClient(canvasBaseURL, token)

	assignments, err := db.AllAssignments()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Fetch submissions concurrently (bounded by Canvas client's rate limiter).
	type assignmentSubs struct {
		slug string
		subs []canvas.Submission
	}
	results := make([]assignmentSubs, len(assignments))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5) // bounded parallelism

	for i, a := range assignments {
		if a.CanvasID == nil {
			continue
		}
		g.Go(func() error {
			subPath := fmt.Sprintf("/courses/%d/assignments/%d/submissions", courseID, *a.CanvasID)
			var canvasSubs []canvas.Submission
			if err := client.GetPaginated(gctx, subPath, nil, &canvasSubs); err != nil {
				return fmt.Errorf("fetch submissions for %s: %w", a.Slug, err)
			}
			results[i] = assignmentSubs{slug: a.Slug, subs: canvasSubs}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var allSubs []SubmissionRow
	for _, r := range results {
		for _, cs := range r.subs {
			submitted := cs.WorkflowState == "submitted" || cs.WorkflowState == "graded" || cs.WorkflowState == "pending_review"
			sub := SubmissionRow{
				StudentID:       cs.UserID,
				AssignmentSlug:  r.slug,
				Source:          sourceCanvas,
				Submitted:       submitted,
				Late:            cs.Late,
				LatenessSeconds: cs.SecondsLate,
				WorkflowState:   sql.NullString{String: cs.WorkflowState, Valid: cs.WorkflowState != ""},
				FetchedAt:       now,
			}
			if cs.SubmittedAt != nil {
				sub.SubmittedAt = sql.NullString{String: *cs.SubmittedAt, Valid: true}
			}
			if cs.Score != nil {
				sub.Score = cs.Score
			}
			allSubs = append(allSubs, sub)
		}
	}

	cl := &Changelog{Entity: "submissions", Added: len(allSubs)}
	if err := db.ReplaceSubmissions(allSubs); err != nil {
		return nil, err
	}

	return cl, nil
}

// --- GitHub Classroom Pull ---

// classroomListEntry is used to parse /classrooms response for ID resolution.
type classroomListEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ResolveClassroomID resolves a GitHub Classroom URL to the API classroom ID.
// The URL contains an org-level numeric ID and a slug (e.g. /classrooms/232475786-test-classroom).
// Multiple classrooms can share the same numeric prefix (it's the org ID, not the classroom ID).
// We resolve by fetching all classrooms and matching the full URL string.
func ResolveClassroomID(ctx context.Context, ghToken, classroomURL string) (int, error) {
	client := github.NewClient(ghToken)
	return resolveClassroomID(ctx, client, classroomURL)
}

func resolveClassroomID(ctx context.Context, client *github.Client, classroomURL string) (int, error) {
	var classrooms []classroomListEntry
	if err := client.GetPaginated(ctx, "/classrooms", nil, &classrooms); err != nil {
		return 0, fmt.Errorf("list classrooms: %w", err)
	}

	// Normalize: strip trailing slash for comparison.
	target := strings.TrimRight(classroomURL, "/")

	for _, c := range classrooms {
		if strings.TrimRight(c.URL, "/") == target {
			return c.ID, nil
		}
	}

	if len(classrooms) == 0 {
		return 0, fmt.Errorf("classroom not found for URL %q — no classrooms found; check your GitHub permissions", classroomURL)
	}

	var names []string
	for _, c := range classrooms {
		names = append(names, fmt.Sprintf("  %s (%s)", c.Name, c.URL))
	}
	return 0, fmt.Errorf("classroom not found for URL %q — check the URL or your GitHub permissions\n\nAvailable classrooms:\n%s", classroomURL, strings.Join(names, "\n"))
}

// PullGHAssignments fetches GitHub Classroom assignments and merges into the assignments table.
func PullGHAssignments(ctx context.Context, db *DB, ghToken string, classroomID int) (*Changelog, error) {
	client := github.NewClient(ghToken)
	path := fmt.Sprintf("/classrooms/%d/assignments", classroomID)
	var ghAssignments []github.Assignment
	if err := client.GetPaginated(ctx, path, nil, &ghAssignments); err != nil {
		return nil, fmt.Errorf("fetch GitHub assignments: %w", err)
	}

	existing, err := db.AllAssignments()
	if err != nil {
		return nil, err
	}
	existingByGHSlug := make(map[string]AssignmentRow, len(existing))
	for _, a := range existing {
		if a.GHSlug.Valid {
			existingByGHSlug[a.GHSlug.String] = a
		}
	}

	cl := &Changelog{Entity: "gh_assignments"}
	for _, ga := range ghAssignments {
		// Try to find existing assignment by gh_slug or by slug match.
		if _, exists := existingByGHSlug[ga.Slug]; exists {
			// Update GH-specific fields on existing row.
			ghPts := float64(ga.Submissions) // Use accepted count as proxy if no points
			err := db.UpdateAssignmentGHFields(ga.Slug, &ghPts, ga.Deadline)
			if err != nil {
				return nil, err
			}
			cl.Updated++
			cl.Details = append(cl.Details, fmt.Sprintf("~ %s (linked)", ga.Title))
		} else {
			// Insert as new assignment (GH-only, no canvas_id).
			row := AssignmentRow{
				Slug:   ga.Slug,
				Title:  ga.Title,
				GHSlug: sql.NullString{String: ga.Slug, Valid: true},
			}
			if ga.Deadline != nil {
				row.GHDeadline = sql.NullString{String: *ga.Deadline, Valid: true}
				row.Deadline = sql.NullString{String: *ga.Deadline, Valid: true}
			}
			if err := db.UpsertAssignments([]AssignmentRow{row}); err != nil {
				return nil, err
			}
			cl.Added++
			cl.Details = append(cl.Details, fmt.Sprintf("+ %s (GitHub only)", ga.Title))
		}
	}

	return cl, nil
}

// PullGHSubmissions fetches GitHub Classroom submissions for all GH-linked assignments.
func PullGHSubmissions(ctx context.Context, db *DB, ghToken string, classroomID int) (*Changelog, error) {
	client := github.NewClient(ghToken)

	// Get GH assignments to fetch accepted_assignments for each.
	path := fmt.Sprintf("/classrooms/%d/assignments", classroomID)
	var ghAssignments []github.Assignment
	if err := client.GetPaginated(ctx, path, nil, &ghAssignments); err != nil {
		return nil, fmt.Errorf("fetch GitHub assignments: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Fetch accepted assignments concurrently (bounded parallelism).
	type assignmentAccepted struct {
		slug     string
		accepted []github.AcceptedAssignment
	}
	results := make([]assignmentAccepted, len(ghAssignments))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(5)

	for i, ga := range ghAssignments {
		g.Go(func() error {
			acceptedPath := fmt.Sprintf("/assignments/%d/accepted_assignments", ga.ID)
			var accepted []github.AcceptedAssignment
			if err := client.GetPaginated(gctx, acceptedPath, nil, &accepted); err != nil {
				return fmt.Errorf("fetch accepted assignments for %s: %w", ga.Slug, err)
			}
			results[i] = assignmentAccepted{slug: ga.Slug, accepted: accepted}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var allSubs []SubmissionRow
	for _, r := range results {
		for _, aa := range r.accepted {
			if len(aa.Students) == 0 {
				continue
			}
			repoName := ""
			if aa.Repository != nil {
				repoName = aa.Repository.FullName
			}

			sub := SubmissionRow{
				StudentID:      aa.Students[0].ID,
				AssignmentSlug: r.slug,
				Source:         sourceGitHub,
				Submitted:      aa.Submitted,
				Passing:        &aa.Passing,
				CommitCount:    &aa.CommitCount,
				RepoName:       sql.NullString{String: repoName, Valid: repoName != ""},
				FetchedAt:      now,
			}
			if aa.Grade != nil {
				sub.GHAutograderScore = sql.NullString{String: *aa.Grade, Valid: true}
			}
			allSubs = append(allSubs, sub)
		}
	}

	cl := &Changelog{Entity: "gh_submissions", Added: len(allSubs)}

	if err := db.UpsertSubmissions(allSubs); err != nil {
		return nil, err
	}

	return cl, nil
}

// GetGHTokenFromCLI retrieves the GitHub token via `gh auth token`.
func GetGHTokenFromCLI() (string, error) {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", fmt.Errorf("run 'gh auth login' first: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned empty — run 'gh auth login'")
	}
	return token, nil
}
