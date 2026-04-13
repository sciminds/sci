package cass

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/sciminds/cli/internal/cass/api/canvas"
	"github.com/sciminds/cli/internal/uikit"
)

// --- Modules ---

// ModulesResult is the output of ListModules.
type ModulesResult struct {
	Modules []canvas.Module `json:"modules"`
}

// JSON implements cmdutil.Result.
func (r *ModulesResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *ModulesResult) Human() string {
	if len(r.Modules) == 0 {
		return "  No modules.\n"
	}
	var b strings.Builder
	for _, m := range r.Modules {
		pub := uikit.TUI.Dim().Render("(draft)")
		if m.Published != nil && *m.Published {
			pub = uikit.TUI.Pass().Render("published")
		}
		fmt.Fprintf(&b, "  %3d  %-30s  %s\n", m.Position, m.Name, pub)
	}
	return b.String()
}

// ListModules fetches all course modules.
func ListModules(ctx context.Context, baseURL, token string, courseID int) (*ModulesResult, error) {
	client := canvas.NewClient(baseURL, token)
	var modules []canvas.Module
	path := fmt.Sprintf("/courses/%d/modules", courseID)
	if err := client.GetPaginated(ctx, path, nil, &modules); err != nil {
		return nil, err
	}
	return &ModulesResult{Modules: modules}, nil
}

// ModuleResult is the output of CreateModule.
type ModuleResult struct {
	Module canvas.Module `json:"module"`
}

// JSON implements cmdutil.Result.
func (r *ModuleResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *ModuleResult) Human() string {
	return fmt.Sprintf("  %s Created module: %s\n", uikit.SymOK, r.Module.Name)
}

// CreateModule creates a new course module.
func CreateModule(ctx context.Context, baseURL, token string, courseID int, name string) (*ModuleResult, error) {
	client := canvas.NewClient(baseURL, token)
	form := canvas.FormData{"module[name]": name}
	path := fmt.Sprintf("/courses/%d/modules", courseID)
	var mod canvas.Module
	if err := client.PostForm(ctx, path, form, &mod); err != nil {
		return nil, err
	}
	return &ModuleResult{Module: mod}, nil
}

// PublishModule publishes or unpublishes a module.
func PublishModule(ctx context.Context, baseURL, token string, courseID, moduleID int, publish bool) (*ModuleResult, error) {
	client := canvas.NewClient(baseURL, token)
	val := "false"
	if publish {
		val = "true"
	}
	form := canvas.FormData{"module[published]": val}
	path := fmt.Sprintf("/courses/%d/modules/%d", courseID, moduleID)
	var mod canvas.Module
	if err := client.PutForm(ctx, path, form, &mod); err != nil {
		return nil, err
	}
	return &ModuleResult{Module: mod}, nil
}

// DeleteModule deletes a module.
func DeleteModule(ctx context.Context, baseURL, token string, courseID, moduleID int) error {
	client := canvas.NewClient(baseURL, token)
	path := fmt.Sprintf("/courses/%d/modules/%d", courseID, moduleID)
	return client.Delete(ctx, path)
}

// --- Assignments ---

// AssignmentsResult is the output of ListCanvasAssignments.
type AssignmentsResult struct {
	Assignments []canvas.Assignment `json:"assignments"`
}

// JSON implements cmdutil.Result.
func (r *AssignmentsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *AssignmentsResult) Human() string {
	if len(r.Assignments) == 0 {
		return "  No assignments.\n"
	}
	var b strings.Builder
	for _, a := range r.Assignments {
		pub := uikit.TUI.Dim().Render("draft")
		if a.Published {
			pub = uikit.TUI.Pass().Render("published")
		}
		due := "-"
		if a.DueAt != nil {
			due = *a.DueAt
		}
		fmt.Fprintf(&b, "  %5d  %-30s  %5.0f pts  %-10s  %s\n", a.ID, a.Name, a.PointsPossible, pub, due)
	}
	return b.String()
}

// ListCanvasAssignments fetches all course assignments.
func ListCanvasAssignments(ctx context.Context, baseURL, token string, courseID int) (*AssignmentsResult, error) {
	client := canvas.NewClient(baseURL, token)
	var assignments []canvas.Assignment
	path := fmt.Sprintf("/courses/%d/assignments", courseID)
	if err := client.GetPaginated(ctx, path, nil, &assignments); err != nil {
		return nil, err
	}
	return &AssignmentsResult{Assignments: assignments}, nil
}

// AssignmentSpec describes an assignment to create or update.
type AssignmentSpec struct {
	Name            string
	Points          float64
	DueAt           string
	Published       bool
	SubmissionTypes []string
	GradingType     string
	GroupID         int
	Description     string
}

// AssignmentResult is the output of CreateCanvasAssignment.
type AssignmentResult struct {
	Assignment canvas.Assignment `json:"assignment"`
}

// JSON implements cmdutil.Result.
func (r *AssignmentResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *AssignmentResult) Human() string {
	return fmt.Sprintf("  %s Created assignment: %s (%.0f pts)\n", uikit.SymOK, r.Assignment.Name, r.Assignment.PointsPossible)
}

// CreateCanvasAssignment creates a new assignment.
func CreateCanvasAssignment(ctx context.Context, baseURL, token string, courseID int, spec AssignmentSpec) (*AssignmentResult, error) {
	client := canvas.NewClient(baseURL, token)
	form := canvas.FormData{
		"assignment[name]": spec.Name,
	}
	if spec.Points > 0 {
		form["assignment[points_possible]"] = fmt.Sprintf("%.0f", spec.Points)
	}
	if spec.DueAt != "" {
		form["assignment[due_at]"] = spec.DueAt
	}
	if spec.Published {
		form["assignment[published]"] = "true"
	}
	if spec.GradingType != "" {
		form["assignment[grading_type]"] = spec.GradingType
	}
	if spec.Description != "" {
		form["assignment[description]"] = spec.Description
	}
	if spec.GroupID > 0 {
		form["assignment[assignment_group_id]"] = fmt.Sprintf("%d", spec.GroupID)
	}

	path := fmt.Sprintf("/courses/%d/assignments", courseID)
	var a canvas.Assignment
	if err := client.PostForm(ctx, path, form, &a); err != nil {
		return nil, err
	}
	return &AssignmentResult{Assignment: a}, nil
}

// UpdateCanvasAssignment updates an existing assignment.
func UpdateCanvasAssignment(ctx context.Context, baseURL, token string, courseID, assignmentID int, spec AssignmentSpec) (*AssignmentResult, error) {
	client := canvas.NewClient(baseURL, token)
	form := make(canvas.FormData)
	if spec.Name != "" {
		form["assignment[name]"] = spec.Name
	}
	if spec.Points > 0 {
		form["assignment[points_possible]"] = fmt.Sprintf("%.0f", spec.Points)
	}
	if spec.DueAt != "" {
		form["assignment[due_at]"] = spec.DueAt
	}
	form["assignment[published]"] = fmt.Sprintf("%t", spec.Published)

	path := fmt.Sprintf("/courses/%d/assignments/%d", courseID, assignmentID)
	var a canvas.Assignment
	if err := client.PutForm(ctx, path, form, &a); err != nil {
		return nil, err
	}
	return &AssignmentResult{Assignment: a}, nil
}

// DeleteCanvasAssignment deletes an assignment.
func DeleteCanvasAssignment(ctx context.Context, baseURL, token string, courseID, assignmentID int) error {
	client := canvas.NewClient(baseURL, token)
	path := fmt.Sprintf("/courses/%d/assignments/%d", courseID, assignmentID)
	return client.Delete(ctx, path)
}

// --- Announcements ---

// AnnouncementResult is the output of PostAnnouncement.
type AnnouncementResult struct {
	Announcement canvas.Announcement `json:"announcement"`
}

// JSON implements cmdutil.Result.
func (r *AnnouncementResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *AnnouncementResult) Human() string {
	return fmt.Sprintf("  %s Posted announcement: %s\n", uikit.SymOK, r.Announcement.Title)
}

// AnnouncementsResult is the output of ListAnnouncements.
type AnnouncementsResult struct {
	Announcements []canvas.Announcement `json:"announcements"`
}

// JSON implements cmdutil.Result.
func (r *AnnouncementsResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *AnnouncementsResult) Human() string {
	if len(r.Announcements) == 0 {
		return "  No announcements.\n"
	}
	var b strings.Builder
	for _, a := range r.Announcements {
		posted := "-"
		if a.PostedAt != nil {
			posted = *a.PostedAt
		}
		fmt.Fprintf(&b, "  %5d  %-40s  %s\n", a.ID, a.Title, posted)
	}
	return b.String()
}

// ListAnnouncements fetches all course announcements.
func ListAnnouncements(ctx context.Context, baseURL, token string, courseID int) (*AnnouncementsResult, error) {
	client := canvas.NewClient(baseURL, token)
	path := fmt.Sprintf("/courses/%d/discussion_topics", courseID)
	params := url.Values{"only_announcements": {"true"}}
	var announcements []canvas.Announcement
	if err := client.GetPaginated(ctx, path, params, &announcements); err != nil {
		return nil, err
	}
	return &AnnouncementsResult{Announcements: announcements}, nil
}

// DeleteAnnouncement deletes an announcement.
func DeleteAnnouncement(ctx context.Context, baseURL, token string, courseID, topicID int) error {
	client := canvas.NewClient(baseURL, token)
	path := fmt.Sprintf("/courses/%d/discussion_topics/%d", courseID, topicID)
	return client.Delete(ctx, path)
}

// PostAnnouncement creates a course announcement.
func PostAnnouncement(ctx context.Context, baseURL, token string, courseID int, title, message string) (*AnnouncementResult, error) {
	client := canvas.NewClient(baseURL, token)
	form := canvas.FormData{
		"title":           title,
		"message":         message,
		"is_announcement": "true",
	}
	path := fmt.Sprintf("/courses/%d/discussion_topics", courseID)
	var a canvas.Announcement
	if err := client.PostForm(ctx, path, form, &a); err != nil {
		return nil, err
	}
	return &AnnouncementResult{Announcement: a}, nil
}

// --- Files ---

// FilesResult is the output of ListFiles.
type FilesResult struct {
	Files []canvas.File `json:"files"`
}

// JSON implements cmdutil.Result.
func (r *FilesResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r *FilesResult) Human() string {
	if len(r.Files) == 0 {
		return "  No files.\n"
	}
	var b strings.Builder
	for _, f := range r.Files {
		size := formatBytes(f.Size)
		fmt.Fprintf(&b, "  %5d  %-40s  %s\n", f.ID, f.DisplayName, size)
	}
	return b.String()
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ListFiles fetches all course files.
func ListFiles(ctx context.Context, baseURL, token string, courseID int) (*FilesResult, error) {
	client := canvas.NewClient(baseURL, token)
	var files []canvas.File
	path := fmt.Sprintf("/courses/%d/files", courseID)
	if err := client.GetPaginated(ctx, path, nil, &files); err != nil {
		return nil, err
	}
	return &FilesResult{Files: files}, nil
}
