package cass

import (
	"database/sql"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/samber/lo"
	_ "modernc.org/sqlite"
)

const schemaVersion = "1"

// DB wraps a SQLite connection for cass data.
type DB struct {
	db   *dbx.DB
	Path string
}

// OpenDB opens (or creates) a cass SQLite database at path.
func OpenDB(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{db: dbx.NewFromDB(sqlDB, "sqlite"), Path: path}
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
	// Check if meta table exists.
	var count int
	err := d.db.NewQuery("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='meta'").Row(&count)
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
		if _, err := d.db.NewQuery(fmt.Sprintf("DROP TABLE IF EXISTS %q", t)).Execute(); err != nil {
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
		if _, err := d.db.NewQuery(stmt).Execute(); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}

	return d.SetMeta("schema_version", schemaVersion)
}

// --- Meta ---

// GetMeta returns the value for a meta key, or "" if not found.
func (d *DB) GetMeta(key string) (string, error) {
	var value string
	err := d.db.NewQuery("SELECT value FROM meta WHERE key={:key}").
		Bind(map[string]any{"key": key}).Row(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

// SetMeta upserts a meta key-value pair.
func (d *DB) SetMeta(key, value string) error {
	_, err := d.db.NewQuery(
		"INSERT INTO meta (key, value) VALUES ({:key}, {:value}) ON CONFLICT(key) DO UPDATE SET value={:value}",
	).Bind(map[string]any{"key": key, "value": value}).Execute()
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

// NullableStudent is used for scanning rows that may have NULL columns.
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
	return d.inTx(func(tx *dbx.Tx) error {
		for _, s := range students {
			_, err := tx.NewQuery(`
				INSERT INTO students (canvas_id, name, sortable_name, email, login_id)
				VALUES ({:canvas_id}, {:name}, {:sortable_name}, {:email}, {:login_id})
				ON CONFLICT(canvas_id) DO UPDATE SET
					name            = {:name},
					sortable_name   = {:sortable_name},
					email           = {:email},
					login_id        = {:login_id}
			`).Bind(map[string]any{
				"canvas_id":     s.CanvasID,
				"name":          s.Name,
				"sortable_name": s.SortableName,
				"email":         s.Email,
				"login_id":      s.LoginID,
			}).Execute()
			if err != nil {
				return fmt.Errorf("upsert student %d: %w", s.CanvasID, err)
			}
		}
		return nil
	})
}

// AllStudents returns all students ordered by name.
func (d *DB) AllStudents() ([]Student, error) {
	var raw []nullableStudent
	err := d.db.NewQuery("SELECT * FROM students ORDER BY name").All(&raw)
	if err != nil {
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
	return d.inTx(func(tx *dbx.Tx) error {
		for _, a := range assignments {
			_, err := tx.NewQuery(`
				INSERT INTO assignments (slug, title, canvas_id, gh_slug, points_possible, gh_points, deadline, gh_deadline, published, assignment_group, post_manually)
				VALUES ({:slug}, {:title}, {:canvas_id}, {:gh_slug}, {:points_possible}, {:gh_points}, {:deadline}, {:gh_deadline}, {:published}, {:assignment_group}, {:post_manually})
				ON CONFLICT(slug) DO UPDATE SET
					title            = {:title},
					canvas_id        = {:canvas_id},
					gh_slug          = {:gh_slug},
					points_possible  = {:points_possible},
					gh_points        = {:gh_points},
					deadline         = {:deadline},
					gh_deadline      = {:gh_deadline},
					published        = {:published},
					assignment_group = {:assignment_group},
					post_manually    = {:post_manually}
			`).Bind(map[string]any{
				"slug":             a.Slug,
				"title":            a.Title,
				"canvas_id":        a.CanvasID,
				"gh_slug":          nullStr(a.GHSlug),
				"points_possible":  a.PointsPossible,
				"gh_points":        a.GHPoints,
				"deadline":         nullStr(a.Deadline),
				"gh_deadline":      nullStr(a.GHDeadline),
				"published":        a.Published,
				"assignment_group": a.AssignmentGroup,
				"post_manually":    a.PostManually,
			}).Execute()
			if err != nil {
				return fmt.Errorf("upsert assignment %q: %w", a.Slug, err)
			}
		}
		return nil
	})
}

// AllAssignments returns all assignments ordered by title.
func (d *DB) AllAssignments() ([]AssignmentRow, error) {
	var rows []AssignmentRow
	err := d.db.NewQuery("SELECT * FROM assignments ORDER BY title").All(&rows)
	return rows, err
}

// UpdateAssignmentGHFields updates GitHub-specific fields on an existing assignment.
func (d *DB) UpdateAssignmentGHFields(ghSlug string, ghPoints *float64, ghDeadline *string) error {
	var dl any
	if ghDeadline != nil {
		dl = *ghDeadline
	}
	_, err := d.db.NewQuery(`
		UPDATE assignments SET gh_points={:gh_points}, gh_deadline={:gh_deadline}
		WHERE gh_slug={:gh_slug}
	`).Bind(map[string]any{"gh_slug": ghSlug, "gh_points": ghPoints, "gh_deadline": dl}).Execute()
	return err
}

// UpsertSubmission inserts or updates a single submission row.
func (d *DB) UpsertSubmission(s SubmissionRow) error {
	return upsertSubmission(d.db, s)
}

// ReplaceSubmissions deletes all existing submissions and inserts new ones.
func (d *DB) ReplaceSubmissions(subs []SubmissionRow) error {
	return d.inTx(func(tx *dbx.Tx) error {
		if _, err := tx.NewQuery("DELETE FROM submissions").Execute(); err != nil {
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
	return d.inTx(func(tx *dbx.Tx) error {
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
	_, err := d.db.NewQuery(
		"UPDATE students SET github_username={:username} WHERE canvas_id={:id}",
	).Bind(map[string]any{"username": username, "id": canvasID}).Execute()
	return err
}

// --- Log ---

// WriteLog records an operation in the log table.
func (d *DB) WriteLog(op, summary, detail string) error {
	_, err := d.db.NewQuery(
		"INSERT INTO log (op, summary, detail) VALUES ({:op}, {:summary}, {:detail})",
	).Bind(map[string]any{"op": op, "summary": summary, "detail": nilIfEmpty(detail)}).Execute()
	return err
}

// ReadLog returns the most recent log entries (newest first).
func (d *DB) ReadLog(limit int) ([]LogEntry, error) {
	var rows []LogEntry
	err := d.db.NewQuery("SELECT * FROM log ORDER BY id DESC LIMIT {:limit}").
		Bind(map[string]any{"limit": limit}).All(&rows)
	return rows, err
}

// inTx runs fn inside a transaction, committing on success or rolling back on error.
func (d *DB) inTx(fn func(tx *dbx.Tx) error) error {
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

func submissionBinds(s SubmissionRow) map[string]any {
	return map[string]any{
		"student_id":          s.StudentID,
		"assignment_slug":     s.AssignmentSlug,
		"source":              s.Source,
		"submitted":           s.Submitted,
		"submitted_at":        nullStr(s.SubmittedAt),
		"late":                s.Late,
		"lateness_seconds":    s.LatenessSeconds,
		"score":               s.Score,
		"workflow_state":      nullStr(s.WorkflowState),
		"repo_name":           nullStr(s.RepoName),
		"commit_count":        s.CommitCount,
		"passing":             s.Passing,
		"gh_autograder_score": nullStr(s.GHAutograderScore),
		"last_commit_at":      nullStr(s.LastCommitAt),
		"last_commit_sha":     nullStr(s.LastCommitSHA),
		"fetched_at":          s.FetchedAt,
	}
}

const submissionCols = `student_id, assignment_slug, source, submitted, submitted_at, late, lateness_seconds, score, workflow_state, repo_name, commit_count, passing, gh_autograder_score, last_commit_at, last_commit_sha, fetched_at`

const submissionVals = `{:student_id}, {:assignment_slug}, {:source}, {:submitted}, {:submitted_at}, {:late}, {:lateness_seconds}, {:score}, {:workflow_state}, {:repo_name}, {:commit_count}, {:passing}, {:gh_autograder_score}, {:last_commit_at}, {:last_commit_sha}, {:fetched_at}`

const submissionUpdateSet = `submitted={:submitted}, submitted_at={:submitted_at}, late={:late}, lateness_seconds={:lateness_seconds}, score={:score}, workflow_state={:workflow_state}, repo_name={:repo_name}, commit_count={:commit_count}, passing={:passing}, gh_autograder_score={:gh_autograder_score}, last_commit_at={:last_commit_at}, last_commit_sha={:last_commit_sha}, fetched_at={:fetched_at}`

func insertSubmission(b dbx.Builder, s SubmissionRow) error {
	_, err := b.NewQuery(`INSERT INTO submissions (` + submissionCols + `) VALUES (` + submissionVals + `)`).
		Bind(submissionBinds(s)).Execute()
	if err != nil {
		return fmt.Errorf("insert submission: %w", err)
	}
	return nil
}

func upsertSubmission(b dbx.Builder, s SubmissionRow) error {
	_, err := b.NewQuery(`INSERT INTO submissions (` + submissionCols + `) VALUES (` + submissionVals + `)
		ON CONFLICT(student_id, assignment_slug, source) DO UPDATE SET ` + submissionUpdateSet).
		Bind(submissionBinds(s)).Execute()
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
