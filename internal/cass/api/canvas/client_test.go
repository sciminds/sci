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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestClient_ConcurrentThrottleSafety(t *testing.T) {
	t.Parallel()
	// Exercises concurrent requests that trigger rate-limit throttling.
	// Run with -race to verify no data races on throttleUntil.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Rate-Limit-Remaining", "10") // below threshold → triggers throttle
		_ = json.NewEncoder(w).Encode(Course{ID: 1})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	c.ThrottleDelay = time.Millisecond // keep test fast

	errs := make(chan error, 10)
	for range 10 {
		go func() {
			var course Course
			errs <- c.Get(context.Background(), "/courses/1", nil, &course)
		}()
	}
	for range 10 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent get: %v", err)
		}
	}
}

func TestClient_RetryExhaustion(t *testing.T) {
	t.Parallel()
	// All retries return 429 → should return error after MaxRetries.
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	c.MaxRetries = 3
	c.RetryBaseDelay = time.Millisecond

	var course Course
	err := c.Get(context.Background(), "/courses/1", nil, &course)
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if callCount.Load() != 3 {
		t.Errorf("call count = %d, want 3 (MaxRetries)", callCount.Load())
	}
}

func TestClient_NonOKStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	var course Course
	err := c.Get(context.Background(), "/courses/1", nil, &course)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error = %q, want it to contain 403", err)
	}
}

func TestClient_GetPaginated_EmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]User{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	var users []User
	if err := c.GetPaginated(context.Background(), "/courses/1/users", nil, &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Errorf("len = %d, want 0", len(users))
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(Course{ID: 1})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var course Course
	err := c.Get(ctx, "/courses/1", nil, &course)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClient_Delete(t *testing.T) {
	t.Parallel()
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	if err := c.Delete(context.Background(), "/courses/1/modules/5"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestClient_BulkGradeFormat(t *testing.T) {
	t.Parallel()
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
