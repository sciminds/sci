// Package canvas provides types and a client for the Canvas LMS REST API.
//
// Types are derived from the OpenAPI 3.1 spec at
// /Users/esh/Documents/webapps/apis/canvas/openapi.yaml
// and cover only the subset of endpoints used by sci cass.
package canvas

// Course is the response from GET /courses/{course_id}.
type Course struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	CourseCode       string `json:"course_code,omitempty"`
	WorkflowState    string `json:"workflow_state,omitempty"`
	DefaultView      string `json:"default_view,omitempty"`
	EnrollmentTermID int    `json:"enrollment_term_id,omitempty"`
	TotalStudents    *int   `json:"total_students,omitempty"`
	TimeZone         string `json:"time_zone,omitempty"`
	GradingStdID     *int   `json:"grading_standard_id,omitempty"`
}

// User is a Canvas user (student/teacher/etc).
type User struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	SortableName string       `json:"sortable_name,omitempty"`
	Email        string       `json:"email,omitempty"`
	SISUserID    *string      `json:"sis_user_id,omitempty"`
	LoginID      string       `json:"login_id,omitempty"`
	Enrollments  []Enrollment `json:"enrollments,omitempty"`
}

// Enrollment represents a user's enrollment in a course.
type Enrollment struct {
	ID              int              `json:"id"`
	UserID          int              `json:"user_id"`
	Type            string           `json:"type"`
	EnrollmentState string           `json:"enrollment_state"`
	Role            string           `json:"role,omitempty"`
	CourseSectionID int              `json:"course_section_id,omitempty"`
	Grades          *EnrollmentGrade `json:"grades,omitempty"`
}

// EnrollmentGrade holds score/grade data from an enrollment.
type EnrollmentGrade struct {
	CurrentScore *float64 `json:"current_score,omitempty"`
	FinalScore   *float64 `json:"final_score,omitempty"`
	CurrentGrade *string  `json:"current_grade,omitempty"`
	FinalGrade   *string  `json:"final_grade,omitempty"`
}

// Section is a course section.
type Section struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	SISSectionID *string `json:"sis_section_id,omitempty"`
}

// Module is a course module.
type Module struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Position   int    `json:"position,omitempty"`
	Published  *bool  `json:"published,omitempty"`
	ItemsCount int    `json:"items_count,omitempty"`
	ItemsURL   string `json:"items_url,omitempty"`
}

// ModuleItem is an item within a module.
type ModuleItem struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	ContentID int    `json:"content_id,omitempty"`
	Position  int    `json:"position,omitempty"`
	Published *bool  `json:"published,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
	ModuleID  int    `json:"module_id,omitempty"`
}

// Assignment is a course assignment.
type Assignment struct {
	ID                      int      `json:"id"`
	Name                    string   `json:"name"`
	PointsPossible          float64  `json:"points_possible"`
	DueAt                   *string  `json:"due_at,omitempty"`
	Published               bool     `json:"published"`
	SubmissionTypes         []string `json:"submission_types,omitempty"`
	GradingType             string   `json:"grading_type,omitempty"`
	AssignmentGroupID       int      `json:"assignment_group_id,omitempty"`
	Position                *int     `json:"position,omitempty"`
	HTMLURL                 string   `json:"html_url,omitempty"`
	Description             *string  `json:"description,omitempty"`
	LockAt                  *string  `json:"lock_at,omitempty"`
	UnlockAt                *string  `json:"unlock_at,omitempty"`
	HasSubmittedSubmissions bool     `json:"has_submitted_submissions,omitempty"`
	WorkflowState           string   `json:"workflow_state,omitempty"`
	PostManually            bool     `json:"post_manually"`
}

// AssignmentGroup is a grade category (e.g. "Labs", "Exams").
type AssignmentGroup struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Position    int     `json:"position,omitempty"`
	GroupWeight float64 `json:"group_weight,omitempty"`
}

// Submission is a student submission for an assignment.
type Submission struct {
	UserID        int      `json:"user_id"`
	SubmittedAt   *string  `json:"submitted_at,omitempty"`
	Late          bool     `json:"late"`
	Missing       bool     `json:"missing"`
	SecondsLate   float64  `json:"seconds_late"`
	Grade         *string  `json:"grade,omitempty"`
	Score         *float64 `json:"score,omitempty"`
	WorkflowState string   `json:"workflow_state"`
}

// Quiz is a course quiz.
type Quiz struct {
	ID            int      `json:"id"`
	Title         string   `json:"title"`
	QuizType      string   `json:"quiz_type,omitempty"`
	Published     bool     `json:"published"`
	TimeLimit     *int     `json:"time_limit,omitempty"`
	QuestionCount int      `json:"question_count,omitempty"`
	PointsPoss    *float64 `json:"points_possible,omitempty"`
	AssignmentID  *int     `json:"assignment_id,omitempty"`
	HTMLURL       string   `json:"html_url,omitempty"`
	Description   *string  `json:"description,omitempty"`
}

// File is a course file.
type File struct {
	ID          int    `json:"id"`
	DisplayName string `json:"display_name"`
	Filename    string `json:"filename,omitempty"`
	Size        int64  `json:"size"`
	ContentType string `json:"content-type,omitempty"`
	URL         string `json:"url,omitempty"`
	FolderID    int    `json:"folder_id,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// Folder is a course folder.
type Folder struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	FullName       string `json:"full_name,omitempty"`
	ParentFolderID *int   `json:"parent_folder_id,omitempty"`
	FilesCount     int    `json:"files_count,omitempty"`
	FoldersCount   int    `json:"folders_count,omitempty"`
	Position       *int   `json:"position,omitempty"`
}

// Announcement is a course announcement (discussion_topic with is_announcement=true).
type Announcement struct {
	ID       int     `json:"id"`
	Title    string  `json:"title"`
	Message  string  `json:"message,omitempty"`
	PostedAt *string `json:"posted_at,omitempty"`
	UserName string  `json:"user_name,omitempty"`
}

// Tab is a course navigation tab.
type Tab struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Type       string `json:"type,omitempty"`
	Position   *int   `json:"position,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Hidden     *bool  `json:"hidden,omitempty"`
}

// Progress tracks an async Canvas operation.
type Progress struct {
	ID            int      `json:"id"`
	WorkflowState string   `json:"workflow_state"`
	Completion    *float64 `json:"completion,omitempty"`
	Message       *string  `json:"message,omitempty"`
	Tag           string   `json:"tag,omitempty"`
	URL           string   `json:"url,omitempty"`
}

// GradingStandard defines a grading scheme.
type GradingStandard struct {
	ID            int           `json:"id"`
	Title         string        `json:"title"`
	GradingScheme []SchemeEntry `json:"grading_scheme,omitempty"`
}

// SchemeEntry is a single grade boundary in a grading scheme.
type SchemeEntry struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}
