package cass

import (
	"database/sql"
	"fmt"

	"github.com/samber/lo"
	_ "modernc.org/sqlite"
)

const schemaVersion = "1"

// execer is satisfied by both *sql.DB and *sql.Tx, letting the submission
// insert/upsert helpers run either standalone or inside a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// DB wraps a SQLite connection for cass data.
type DB struct {
	db   *sql.DB
	Path string
}

// OpenDB opens (or creates) a cass SQLite database at path.
func OpenDB(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{db: sqlDB, Path: path}
	if err := d.ensureSchema(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// ensureSchema creates tables if missing or recreates on version mismatch.
func (d *DB) ensureSchema() error {
	var count int
	err := d.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		ver, err := d.GetMeta("schema_version")
		if err != nil {
			return err
		}
		if ver == schemaVersion {
			return nil // schema up to date
		}
		// Version mismatch — drop and recreate.
		if err := d.dropAll(); err != nil {
			return err
		}
	}

	return d.createSchema()
}

func (d *DB) dropAll() error {
	tables := []string{"students", "assignments", "submissions", "grades", "_grades_synced", "log", "meta"}
	for _, t := range tables {
		if _, err := d.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q", t)); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) createSchema() error {
	ddl := []string{
		`CREATE TABLE IF NOT EXISTS students (
			canvas_id       INTEGER PRIMARY KEY,
			name            TEXT NOT NULL,
			sortable_name   TEXT NOT NULL DEFAULT '',
			email           TEXT NOT NULL DEFAULT '',
			login_id        TEXT NOT NULL DEFAULT '',
			github_username TEXT,
			excluded        INTEGER NOT NULL DEFAULT 0,
			UNIQUE(github_username)
		)`,
		`CREATE TABLE IF NOT EXISTS assignments (
			slug              TEXT PRIMARY KEY,
			title             TEXT NOT NULL,
			canvas_id         INTEGER UNIQUE,
			gh_slug           TEXT UNIQUE,
			points_possible   REAL NOT NULL DEFAULT 0,
			gh_points         REAL,
			deadline          TEXT,
			gh_deadline       TEXT,
			published         INTEGER NOT NULL DEFAULT 0,
			assignment_group  TEXT NOT NULL DEFAULT '',
			post_manually     INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS submissions (
			student_id       INTEGER NOT NULL,
			assignment_slug  TEXT NOT NULL,
			source           TEXT NOT NULL CHECK(source IN ('canvas','github')),
			submitted        INTEGER NOT NULL DEFAULT 0,
			submitted_at     TEXT,
			late             INTEGER NOT NULL DEFAULT 0,
			lateness_seconds REAL NOT NULL DEFAULT 0,
			score            REAL,
			workflow_state   TEXT,
			repo_name        TEXT,
			commit_count     INTEGER,
			passing          INTEGER,
			gh_autograder_score TEXT,
			last_commit_at   TEXT,
			last_commit_sha  TEXT,
			fetched_at       TEXT NOT NULL,
			PRIMARY KEY (student_id, assignment_slug, source)
		)`,
		`CREATE TABLE IF NOT EXISTS grades (
			student_id           INTEGER NOT NULL,
			assignment_slug      TEXT NOT NULL,
			canvas_user_id       INTEGER NOT NULL,
			canvas_assignment_id INTEGER NOT NULL,
			posted_grade         TEXT NOT NULL DEFAULT '',
			updated_at           TEXT NOT NULL,
			PRIMARY KEY (student_id, assignment_slug)
		)`,
		`CREATE TABLE IF NOT EXISTS _grades_synced (
			student_id           INTEGER NOT NULL,
			assignment_slug      TEXT NOT NULL,
			canvas_user_id       INTEGER NOT NULL,
			canvas_assignment_id INTEGER NOT NULL,
			posted_grade         TEXT NOT NULL DEFAULT '',
			synced_at            TEXT NOT NULL,
			PRIMARY KEY (student_id, assignment_slug)
		)`,
		`CREATE TABLE IF NOT EXISTS log (
			id         INTEGER PRIMARY KEY,
			op         TEXT NOT NULL,
			summary    TEXT NOT NULL,
			detail     TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, stmt := range ddl {
		if _, err := d.db.Exec(stmt); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}

	return d.SetMeta("schema_version", schemaVersion)
}

// --- Meta ---

// GetMeta returns the value for a meta key, or "" if not found.
func (d *DB) GetMeta(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetMeta upserts a meta key-value pair.
func (d *DB) SetMeta(key, value string) error {
	_, err := d.db.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

// --- Domain types ---

// Student is a row in the students table.
type Student struct {
	CanvasID       int    `db:"canvas_id"`
	Name           string `db:"name"`
	SortableName   string `db:"sortable_name"`
	Email          string `db:"email"`
	LoginID        string `db:"login_id"`
	GitHubUsername string `db:"github_username" json:"github_username,omitempty"`
	Excluded       bool   `db:"excluded"`
}

// nullableStudent is used for scanning rows that may have NULL columns.
// Use Student for API/logic code; this is an internal scan target.
type nullableStudent struct {
	CanvasID       int            `db:"canvas_id"`
	Name           string         `db:"name"`
	SortableName   string         `db:"sortable_name"`
	Email          string         `db:"email"`
	LoginID        string         `db:"login_id"`
	GitHubUsername sql.NullString `db:"github_username"`
	Excluded       bool           `db:"excluded"`
}

func (n nullableStudent) toStudent() Student {
	return Student{
		CanvasID:       n.CanvasID,
		Name:           n.Name,
		SortableName:   n.SortableName,
		Email:          n.Email,
		LoginID:        n.LoginID,
		GitHubUsername: n.GitHubUsername.String,
		Excluded:       n.Excluded,
	}
}

// AssignmentRow is a row in the assignments table.
type AssignmentRow struct {
	Slug            string         `db:"slug"`
	Title           string         `db:"title"`
	CanvasID        *int           `db:"canvas_id"`
	GHSlug          sql.NullString `db:"gh_slug"`
	PointsPossible  float64        `db:"points_possible"`
	GHPoints        *float64       `db:"gh_points"`
	Deadline        sql.NullString `db:"deadline"`
	GHDeadline      sql.NullString `db:"gh_deadline"`
	Published       bool           `db:"published"`
	AssignmentGroup string         `db:"assignment_group"`
	PostManually    bool           `db:"post_manually"`
}

// SubmissionRow is a row in the submissions table.
type SubmissionRow struct {
	StudentID         int            `db:"student_id"`
	AssignmentSlug    string         `db:"assignment_slug"`
	Source            string         `db:"source"`
	Submitted         bool           `db:"submitted"`
	SubmittedAt       sql.NullString `db:"submitted_at"`
	Late              bool           `db:"late"`
	LatenessSeconds   float64        `db:"lateness_seconds"`
	Score             *float64       `db:"score"`
	WorkflowState     sql.NullString `db:"workflow_state"`
	RepoName          sql.NullString `db:"repo_name"`
	CommitCount       *int           `db:"commit_count"`
	Passing           *bool          `db:"passing"`
	GHAutograderScore sql.NullString `db:"gh_autograder_score"`
	LastCommitAt      sql.NullString `db:"last_commit_at"`
	LastCommitSHA     sql.NullString `db:"last_commit_sha"`
	FetchedAt         string         `db:"fetched_at"`
}

// GradeRow is a row in the grades table.
type GradeRow struct {
	StudentID          int    `db:"student_id"`
	AssignmentSlug     string `db:"assignment_slug"`
	CanvasUserID       int    `db:"canvas_user_id"`
	CanvasAssignmentID int    `db:"canvas_assignment_id"`
	PostedGrade        string `db:"posted_grade"`
	UpdatedAt          string `db:"updated_at"`
}

// LogEntry is a row in the log table.
type LogEntry struct {
	ID        int            `db:"id"`
	Op        string         `db:"op"`
	Summary   string         `db:"summary"`
	Detail    sql.NullString `db:"detail"`
	CreatedAt string         `db:"created_at"`
}

// --- CRUD ---

// UpsertStudents inserts or updates students, preserving local columns
// (github_username, excluded).
func (d *DB) UpsertStudents(students []Student) error {
	if len(students) == 0 {
		return nil
	}
	return d.inTx(func(tx *sql.Tx) error {
		const q = `
			INSERT INTO students (canvas_id, name, sortable_name, email, login_id)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(canvas_id) DO UPDATE SET
				name            = excluded.name,
				sortable_name   = excluded.sortable_name,
				email           = excluded.email,
				login_id        = excluded.login_id`
		for _, s := range students {
			if _, err := tx.Exec(q, s.CanvasID, s.Name, s.SortableName, s.Email, s.LoginID); err != nil {
				return fmt.Errorf("upsert student %d: %w", s.CanvasID, err)
			}
		}
		return nil
	})
}

// AllStudents returns all students ordered by name.
func (d *DB) AllStudents() ([]Student, error) {
	rows, err := d.db.Query(`
		SELECT canvas_id, name, sortable_name, email, login_id, github_username, excluded
		FROM students ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var raw []nullableStudent
	for rows.Next() {
		var n nullableStudent
		if err := rows.Scan(&n.CanvasID, &n.Name, &n.SortableName, &n.Email, &n.LoginID, &n.GitHubUsername, &n.Excluded); err != nil {
			return nil, err
		}
		raw = append(raw, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lo.Map(raw, func(r nullableStudent, _ int) Student {
		return r.toStudent()
	}), nil
}

// UpsertAssignments inserts or updates assignments.
func (d *DB) UpsertAssignments(assignments []AssignmentRow) error {
	if len(assignments) == 0 {
		return nil
	}
	return d.inTx(func(tx *sql.Tx) error {
		const q = `
			INSERT INTO assignments (slug, title, canvas_id, gh_slug, points_possible, gh_points, deadline, gh_deadline, published, assignment_group, post_manually)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(slug) DO UPDATE SET
				title            = excluded.title,
				canvas_id        = excluded.canvas_id,
				gh_slug          = excluded.gh_slug,
				points_possible  = excluded.points_possible,
				gh_points        = excluded.gh_points,
				deadline         = excluded.deadline,
				gh_deadline      = excluded.gh_deadline,
				published        = excluded.published,
				assignment_group = excluded.assignment_group,
				post_manually    = excluded.post_manually`
		for _, a := range assignments {
			if _, err := tx.Exec(q,
				a.Slug,
				a.Title,
				a.CanvasID,
				nullStr(a.GHSlug),
				a.PointsPossible,
				a.GHPoints,
				nullStr(a.Deadline),
				nullStr(a.GHDeadline),
				a.Published,
				a.AssignmentGroup,
				a.PostManually,
			); err != nil {
				return fmt.Errorf("upsert assignment %q: %w", a.Slug, err)
			}
		}
		return nil
	})
}

// AllAssignments returns all assignments ordered by title.
func (d *DB) AllAssignments() ([]AssignmentRow, error) {
	sqlRows, err := d.db.Query(`
		SELECT slug, title, canvas_id, gh_slug, points_possible, gh_points,
			deadline, gh_deadline, published, assignment_group, post_manually
		FROM assignments ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	var rows []AssignmentRow
	for sqlRows.Next() {
		var (
			a        AssignmentRow
			canvasID sql.NullInt64
			ghPoints sql.NullFloat64
		)
		if err := sqlRows.Scan(
			&a.Slug,
			&a.Title,
			&canvasID,
			&a.GHSlug,
			&a.PointsPossible,
			&ghPoints,
			&a.Deadline,
			&a.GHDeadline,
			&a.Published,
			&a.AssignmentGroup,
			&a.PostManually,
		); err != nil {
			return nil, err
		}
		if canvasID.Valid {
			v := int(canvasID.Int64)
			a.CanvasID = &v
		}
		if ghPoints.Valid {
			v := ghPoints.Float64
			a.GHPoints = &v
		}
		rows = append(rows, a)
	}
	return rows, sqlRows.Err()
}

// UpdateAssignmentGHFields updates GitHub-specific fields on an existing assignment.
func (d *DB) UpdateAssignmentGHFields(ghSlug string, ghPoints *float64, ghDeadline *string) error {
	var dl any
	if ghDeadline != nil {
		dl = *ghDeadline
	}
	_, err := d.db.Exec(
		`UPDATE assignments SET gh_points = ?, gh_deadline = ? WHERE gh_slug = ?`,
		ghPoints, dl, ghSlug,
	)
	return err
}

// UpsertSubmission inserts or updates a single submission row.
func (d *DB) UpsertSubmission(s SubmissionRow) error {
	return upsertSubmission(d.db, s)
}

// ReplaceSubmissions deletes all existing submissions and inserts new ones.
func (d *DB) ReplaceSubmissions(subs []SubmissionRow) error {
	return d.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM submissions"); err != nil {
			return err
		}
		for _, s := range subs {
			if err := insertSubmission(tx, s); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertSubmissions inserts or updates multiple submissions in a single transaction.
func (d *DB) UpsertSubmissions(subs []SubmissionRow) error {
	if len(subs) == 0 {
		return nil
	}
	return d.inTx(func(tx *sql.Tx) error {
		for _, s := range subs {
			if err := upsertSubmission(tx, s); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetStudentGitHubUsername sets the github_username for a student by canvas_id.
func (d *DB) SetStudentGitHubUsername(canvasID int, username string) error {
	_, err := d.db.Exec(
		"UPDATE students SET github_username = ? WHERE canvas_id = ?",
		username, canvasID,
	)
	return err
}

// --- Log ---

// WriteLog records an operation in the log table.
func (d *DB) WriteLog(op, summary, detail string) error {
	_, err := d.db.Exec(
		"INSERT INTO log (op, summary, detail) VALUES (?, ?, ?)",
		op, summary, nilIfEmpty(detail),
	)
	return err
}

// ReadLog returns the most recent log entries (newest first).
func (d *DB) ReadLog(limit int) ([]LogEntry, error) {
	sqlRows, err := d.db.Query(
		"SELECT id, op, summary, detail, created_at FROM log ORDER BY id DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlRows.Close() }()

	var rows []LogEntry
	for sqlRows.Next() {
		var e LogEntry
		if err := sqlRows.Scan(&e.ID, &e.Op, &e.Summary, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}
		rows = append(rows, e)
	}
	return rows, sqlRows.Err()
}

// inTx runs fn inside a transaction, committing on success or rolling back on error.
func (d *DB) inTx(fn func(tx *sql.Tx) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// submissionArgs returns the positional argument list for inserting or
// upserting a submission row.
func submissionArgs(s SubmissionRow) []any {
	return []any{
		s.StudentID,
		s.AssignmentSlug,
		s.Source,
		s.Submitted,
		nullStr(s.SubmittedAt),
		s.Late,
		s.LatenessSeconds,
		s.Score,
		nullStr(s.WorkflowState),
		nullStr(s.RepoName),
		s.CommitCount,
		s.Passing,
		nullStr(s.GHAutograderScore),
		nullStr(s.LastCommitAt),
		nullStr(s.LastCommitSHA),
		s.FetchedAt,
	}
}

const submissionCols = `student_id, assignment_slug, source, submitted, submitted_at, late, lateness_seconds, score, workflow_state, repo_name, commit_count, passing, gh_autograder_score, last_commit_at, last_commit_sha, fetched_at`

const submissionPlaceholders = `?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`

const submissionUpdateSet = `submitted=excluded.submitted, submitted_at=excluded.submitted_at, late=excluded.late, lateness_seconds=excluded.lateness_seconds, score=excluded.score, workflow_state=excluded.workflow_state, repo_name=excluded.repo_name, commit_count=excluded.commit_count, passing=excluded.passing, gh_autograder_score=excluded.gh_autograder_score, last_commit_at=excluded.last_commit_at, last_commit_sha=excluded.last_commit_sha, fetched_at=excluded.fetched_at`

func insertSubmission(e execer, s SubmissionRow) error {
	_, err := e.Exec(
		`INSERT INTO submissions (`+submissionCols+`) VALUES (`+submissionPlaceholders+`)`,
		submissionArgs(s)...,
	)
	if err != nil {
		return fmt.Errorf("insert submission: %w", err)
	}
	return nil
}

func upsertSubmission(e execer, s SubmissionRow) error {
	_, err := e.Exec(
		`INSERT INTO submissions (`+submissionCols+`) VALUES (`+submissionPlaceholders+`)
			ON CONFLICT(student_id, assignment_slug, source) DO UPDATE SET `+submissionUpdateSet,
		submissionArgs(s)...,
	)
	return err
}

// nilIfEmpty returns nil if s is empty, or s otherwise.
// Useful for inserting NULL instead of empty strings.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullStr converts a sql.NullString to a bind-safe value (nil or string).
func nullStr(ns sql.NullString) any {
	if !ns.Valid {
		return nil
	}
	return ns.String
}
