package cass

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sciminds/cli/internal/cass/api/canvas"
)

func TestListModules(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mods := []canvas.Module{
			{ID: 1, Name: "Week 1", Position: 1},
			{ID: 2, Name: "Week 2", Position: 2},
		}
		_ = json.NewEncoder(w).Encode(mods)
	}))
	defer srv.Close()

	result, err := ListModules(context.Background(), srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Modules) != 2 {
		t.Fatalf("len = %d, want 2", len(result.Modules))
	}
	if result.Modules[0].Name != "Week 1" {
		t.Errorf("name = %q", result.Modules[0].Name)
	}
}

func TestCreateModule(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		_ = r.ParseForm()
		if r.Form.Get("module[name]") != "Week 3" {
			t.Errorf("name = %q", r.Form.Get("module[name]"))
		}
		_ = json.NewEncoder(w).Encode(canvas.Module{ID: 3, Name: "Week 3"})
	}))
	defer srv.Close()

	result, err := CreateModule(context.Background(), srv.URL, "token", 1, "Week 3")
	if err != nil {
		t.Fatal(err)
	}
	if result.Module.Name != "Week 3" {
		t.Errorf("name = %q", result.Module.Name)
	}
}

func TestListAssignmentsCanvas(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		assigns := []canvas.Assignment{
			{ID: 101, Name: "Lab 1", PointsPossible: 20, Published: true},
			{ID: 102, Name: "Lab 2", PointsPossible: 25},
		}
		_ = json.NewEncoder(w).Encode(assigns)
	}))
	defer srv.Close()

	result, err := ListCanvasAssignments(context.Background(), srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assignments) != 2 {
		t.Fatalf("len = %d", len(result.Assignments))
	}
}

func TestCreateAssignment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("assignment[name]") != "Lab 3" {
			t.Errorf("name = %q", r.Form.Get("assignment[name]"))
		}
		if r.Form.Get("assignment[points_possible]") != "30" {
			t.Errorf("points = %q", r.Form.Get("assignment[points_possible]"))
		}
		_ = json.NewEncoder(w).Encode(canvas.Assignment{ID: 103, Name: "Lab 3", PointsPossible: 30})
	}))
	defer srv.Close()

	result, err := CreateCanvasAssignment(context.Background(), srv.URL, "token", 1, AssignmentSpec{
		Name:   "Lab 3",
		Points: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Assignment.Name != "Lab 3" {
		t.Errorf("name = %q", result.Assignment.Name)
	}
}

func TestPostAnnouncement(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("title") != "Hello" {
			t.Errorf("title = %q", r.Form.Get("title"))
		}
		if r.Form.Get("is_announcement") != "true" {
			t.Errorf("is_announcement = %q", r.Form.Get("is_announcement"))
		}
		_ = json.NewEncoder(w).Encode(canvas.Announcement{ID: 1, Title: "Hello"})
	}))
	defer srv.Close()

	result, err := PostAnnouncement(context.Background(), srv.URL, "token", 1, "Hello", "Welcome!")
	if err != nil {
		t.Fatal(err)
	}
	if result.Announcement.Title != "Hello" {
		t.Errorf("title = %q", result.Announcement.Title)
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{2621440, "2.5 MB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListFiles(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		files := []canvas.File{
			{ID: 1, DisplayName: "syllabus.pdf", Size: 1024},
			{ID: 2, DisplayName: "data.csv", Size: 2048},
		}
		_ = json.NewEncoder(w).Encode(files)
	}))
	defer srv.Close()

	result, err := ListFiles(context.Background(), srv.URL, "token", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("len = %d", len(result.Files))
	}
}
