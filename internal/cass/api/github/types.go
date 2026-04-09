// Package github provides types and a client for the GitHub Classroom API.
//
// Types are derived from the OpenAPI 3.1 spec at
// /Users/esh/Documents/webapps/apis/github-classroom/openapi.yaml
// and cover only the subset of endpoints used by sci cass.
package github

// Assignment is a GitHub Classroom assignment.
type Assignment struct {
	ID                    int                    `json:"id"`
	Slug                  string                 `json:"slug"`
	Title                 string                 `json:"title"`
	Deadline              *string                `json:"deadline,omitempty"`
	Accepted              int                    `json:"accepted"`
	Submissions           int                    `json:"submissions"`
	Passing               int                    `json:"passing"`
	StarterCodeRepository *StarterCodeRepository `json:"starter_code_repository,omitempty"`
}

// StarterCodeRepository is a template repo for an assignment.
type StarterCodeRepository struct {
	ID       int    `json:"id"`
	FullName string `json:"full_name"`
}

// AcceptedAssignment is a student's accepted assignment (their repo).
type AcceptedAssignment struct {
	ID          int          `json:"id"`
	Students    []StudentRef `json:"students"`
	Repository  *Repository  `json:"repository,omitempty"`
	CommitCount int          `json:"commit_count"`
	Submitted   bool         `json:"submitted"`
	Passing     bool         `json:"passing"`
	Grade       *string      `json:"grade,omitempty"`
}

// StudentRef is a minimal student reference.
type StudentRef struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
}

// Repository is a GitHub repository reference.
type Repository struct {
	ID       int    `json:"id"`
	FullName string `json:"full_name"`
}

// RosterEntry is a row from the assignment grades export.
type RosterEntry struct {
	GitHubUsername      string `json:"github_username"`
	RosterIdentifier    string `json:"roster_identifier"`
	StudentRepoName     string `json:"student_repository_name"`
	StudentRepoURL      string `json:"student_repository_url"`
	SubmissionTimestamp string `json:"submission_timestamp"`
	PointsAwarded       string `json:"points_awarded"`
	PointsAvailable     string `json:"points_available"`
}

// Commit is a GitHub commit.
type Commit struct {
	SHA    string     `json:"sha"`
	Commit CommitInfo `json:"commit"`
}

// CommitInfo holds the inner commit metadata.
type CommitInfo struct {
	Committer Committer `json:"committer"`
}

// Committer holds committer metadata.
type Committer struct {
	Date string `json:"date"`
}

// ContentItem is a file or directory in a repository.
type ContentItem struct {
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Path        string  `json:"path,omitempty"`
	SHA         string  `json:"sha,omitempty"`
	Size        int     `json:"size,omitempty"`
	URL         string  `json:"url,omitempty"`
	HTMLURL     string  `json:"html_url,omitempty"`
	GitURL      string  `json:"git_url,omitempty"`
	DownloadURL *string `json:"download_url,omitempty"`
	Content     string  `json:"content,omitempty"`
	Encoding    string  `json:"encoding,omitempty"`
}

// Error is a GitHub API error response.
type Error struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url,omitempty"`
}
