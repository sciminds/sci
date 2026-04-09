package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("bad auth header: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(Course{ID: 1, Name: "Test Course"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	var course Course
	if err := c.Get(context.Background(), "/courses/1", nil, &course); err != nil {
		t.Fatal(err)
	}
	if course.Name != "Test Course" {
		t.Errorf("name = %q", course.Name)
	}
}

func TestClient_GetPaginated(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		users := []User{{ID: page, Name: fmt.Sprintf("User %d", page)}}
		if page < 3 {
			nextURL := fmt.Sprintf("http://%s%s?page=%d&per_page=1", r.Host, r.URL.Path, page+1)
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, nextURL))
		}
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	var users []User
	if err := c.GetPaginated(context.Background(), "/courses/1/users", nil, &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("len = %d, want 3", len(users))
	}
	if users[2].Name != "User 3" {
		t.Errorf("last user = %q", users[2].Name)
	}
}

func TestClient_RateLimitThrottle(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.Header().Set("X-Rate-Limit-Remaining", "10") // below threshold
		_ = json.NewEncoder(w).Encode(Course{ID: 1})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	c.ThrottleDelay = 10 * time.Millisecond // speed up for test

	start := time.Now()
	var course Course
	if err := c.Get(context.Background(), "/courses/1", nil, &course); err != nil {
		t.Fatal(err)
	}
	// Second call should be delayed.
	if err := c.Get(context.Background(), "/courses/2", nil, &course); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("expected throttle delay, elapsed = %v", elapsed)
	}
}

func TestClient_Retry429(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(Course{ID: 1, Name: "OK"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	c.RetryBaseDelay = time.Millisecond // speed up

	var course Course
	if err := c.Get(context.Background(), "/courses/1", nil, &course); err != nil {
		t.Fatal(err)
	}
	if course.Name != "OK" {
		t.Errorf("name = %q", course.Name)
	}
	if callCount.Load() != 2 {
		t.Errorf("call count = %d, want 2", callCount.Load())
	}
}

func TestClient_PostForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Errorf("content-type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("module[name]") != "Week 1" {
			t.Errorf("module[name] = %q", r.Form.Get("module[name]"))
		}
		if r.Form.Get("module[position]") != "1" {
			t.Errorf("module[position] = %q", r.Form.Get("module[position]"))
		}
		_ = json.NewEncoder(w).Encode(Module{ID: 1, Name: "Week 1"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	form := FormData{
		"module[name]":     "Week 1",
		"module[position]": "1",
	}
	var mod Module
	if err := c.PostForm(context.Background(), "/courses/1/modules", form, &mod); err != nil {
		t.Fatal(err)
	}
	if mod.Name != "Week 1" {
		t.Errorf("name = %q", mod.Name)
	}
}

func TestClient_BulkGradeFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		// Check bracket notation: grade_data[123][posted_grade]=A
		if r.Form.Get("grade_data[123][posted_grade]") != "A" {
			t.Errorf("grade for 123 = %q", r.Form.Get("grade_data[123][posted_grade]"))
		}
		if r.Form.Get("grade_data[456][posted_grade]") != "B+" {
			t.Errorf("grade for 456 = %q", r.Form.Get("grade_data[456][posted_grade]"))
		}
		_ = json.NewEncoder(w).Encode(Progress{ID: 1, WorkflowState: "queued"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	grades := map[int]string{123: "A", 456: "B+"}
	form := BulkGradeForm(grades)
	var prog Progress
	if err := c.PostForm(context.Background(), "/courses/1/assignments/10/submissions/update_grades", form, &prog); err != nil {
		t.Fatal(err)
	}
	if prog.WorkflowState != "queued" {
		t.Errorf("state = %q", prog.WorkflowState)
	}
}
