package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
)

// The `sci db` verbs must speak the closed cmdutil.Code vocabulary: a
// mistyped command line codes usage (exit 2), a missing thing codes
// not-found, and every failure a caller can act on carries fix or try.
// These tests pin the SQLite paths; the duckdb paths share the same
// classification helpers.

// newProbeDB returns a small writable SQLite database with one table, t.
func newProbeDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.db")
	if _, err := Create(path); err != nil {
		t.Fatalf("Create: %v", err)
	}
	csv := filepath.Join(filepath.Dir(path), "t.csv")
	if err := os.WriteFile(csv, []byte("id,name\n1,a\n2,b\n"), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if _, err := AddCSV([]string{csv}, path, "t"); err != nil {
		t.Fatalf("AddCSV: %v", err)
	}
	return path
}

// TestResetRefusesANonDatabase — `sci db reset` deletes the file it is
// pointed at, so a mistyped path must not cost the user a document.
func TestResetRefusesANonDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thesis.docx")
	body := []byte("PK\x03\x04 not a database, and not disposable\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	coded := codedFrom(t, mustFailReset(t, path))
	if coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Reset deleted the file it refused: %v", err)
	}
	if string(after) != string(body) {
		t.Error("Reset modified the file it refused")
	}
}

// TestViewRefusesANonDatabase — the TUI entry point classifies before it
// opens anything, so `sci view notes.txt` explains itself instead of
// leaking a pragma failure.
func TestViewRefusesANonDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("plain text\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := RunTUI(path, "")
	if err == nil {
		t.Fatal("RunTUI accepted a text file")
	}
	if coded := codedFrom(t, err); coded.Code != cmdutil.CodeUsage {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if coded := codedFrom(t, RunTUI(missing, "")); coded.Code != cmdutil.CodeNotFound {
		t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeNotFound)
	}
}

// mustFailReset runs Reset and requires it to fail.
func mustFailReset(t *testing.T, path string) error {
	t.Helper()
	result, err := Reset(path)
	if err == nil {
		t.Fatalf("Reset(%q) unexpectedly succeeded: %+v", path, result)
	}
	return err
}

func TestErrorCodes(t *testing.T) {
	probe := newProbeDB(t)
	csv := filepath.Join(filepath.Dir(probe), "t.csv")
	missing := filepath.Join(t.TempDir(), "missing.db")

	cases := []struct {
		name         string
		run          func() error
		want         cmdutil.Code
		wantGuidance bool
	}{
		{
			name:         "delete from a missing database",
			run:          func() error { _, err := DeleteTable("t", missing); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "delete a table that does not exist",
			run:          func() error { _, err := DeleteTable("nosuch", probe); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "delete with an unusable table name",
			run:          func() error { _, err := DeleteTable("bad;name", probe); return err },
			want:         cmdutil.CodeUsage,
			wantGuidance: false,
		},
		{
			name:         "rename a table that does not exist",
			run:          func() error { _, err := RenameTable("nosuch", "other", probe); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "rename onto a name already taken",
			run:          func() error { _, err := RenameTable("t", "t", probe); return err },
			want:         cmdutil.CodeUsage,
			wantGuidance: true,
		},
		{
			name:         "add a CSV that does not exist",
			run:          func() error { _, err := AddCSV([]string{"nope.csv"}, probe, ""); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "add into a database that does not exist",
			run:          func() error { _, err := AddCSV([]string{csv}, missing, ""); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "add a table that already exists",
			run:          func() error { _, err := AddCSV([]string{csv}, probe, "t"); return err },
			want:         cmdutil.CodeUsage,
			wantGuidance: true,
		},
		{
			name:         "append to a table that does not exist",
			run:          func() error { _, err := AppendCSV([]string{csv}, probe, "nosuch"); return err },
			want:         cmdutil.CodeNotFound,
			wantGuidance: true,
		},
		{
			name:         "--table with more than one CSV",
			run:          func() error { _, err := AddCSV([]string{csv, csv}, probe, "t"); return err },
			want:         cmdutil.CodeUsage,
			wantGuidance: false,
		},
		{
			name:         "create over a file that already exists",
			run:          func() error { _, err := Create(probe); return err },
			want:         cmdutil.CodeUsage,
			wantGuidance: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coded := codedFrom(t, tc.run())
			if coded.Code != tc.want {
				t.Errorf("code = %q, want %q (message: %s)", coded.Code, tc.want, coded.Message)
			}
			if tc.wantGuidance && coded.Fix == "" && coded.Try == "" {
				t.Errorf("no fix/try on an actionable failure: %s", coded.Message)
			}
			if strings.Contains(coded.Message, "SQL logic error") {
				t.Errorf("message %q leaks the SQLite driver's wording", coded.Message)
			}
		})
	}
}
