package openalex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildSearchParams(t *testing.T) {
	t.Parallel()
	got := buildSearchParams(SearchOpts{
		Search:  "attention is all you need",
		Filter:  map[string]string{"from_publication_date": "2017-01-01", "type": "article"},
		PerPage: 50,
		Page:    2,
		Sort:    "cited_by_count:desc",
		Select:  []string{"id", "doi", "title"},
	})
	if got.Get("search") != "attention is all you need" {
		t.Errorf("search = %q", got.Get("search"))
	}
	if got.Get("per_page") != "50" {
		t.Errorf("per_page = %q", got.Get("per_page"))
	}
	if got.Get("page") != "2" {
		t.Errorf("page = %q", got.Get("page"))
	}
	if got.Get("sort") != "cited_by_count:desc" {
		t.Errorf("sort = %q", got.Get("sort"))
	}
	if got.Get("select") != "id,doi,title" {
		t.Errorf("select = %q", got.Get("select"))
	}
	// Filter is a DSL of comma-joined key:value pairs — order must be stable.
	f := got.Get("filter")
	if !strings.Contains(f, "from_publication_date:2017-01-01") || !strings.Contains(f, "type:article") {
		t.Errorf("filter = %q", f)
	}
	if strings.Count(f, ",") != 1 {
		t.Errorf("filter must be comma-joined, got %q", f)
	}
}

func TestBuildSearchParams_defaults(t *testing.T) {
	t.Parallel()
	got := buildSearchParams(SearchOpts{Search: "hello"})
	if got.Get("per_page") != "25" {
		t.Errorf("default per_page = %q, want 25", got.Get("per_page"))
	}
	if got.Has("page") || got.Has("cursor") || got.Has("filter") || got.Has("sort") || got.Has("select") {
		t.Errorf("unexpected keys: %v", got)
	}
}

func TestClient_SearchWorks(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("search") != "transformers" {
			t.Errorf("search = %q", r.URL.Query().Get("search"))
		}
		title := "Attention Is All You Need"
		_ = json.NewEncoder(w).Encode(Results[Work]{
			Meta:    ResultsMeta{Count: 1, PerPage: 25},
			Results: []Work{{ID: "W1", Title: &title}},
		})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	res, err := c.SearchWorks(context.Background(), SearchOpts{Search: "transformers"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].Title == nil || *res.Results[0].Title != "Attention Is All You Need" {
		t.Errorf("got %+v", res)
	}
}

func TestClient_IterateWorks_walksCursor(t *testing.T) {
	t.Parallel()
	// Two-page cursor walk: initial cursor=* returns 2 items + next_cursor="c2";
	// cursor=c2 returns 1 item + null next_cursor (terminal).
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		cur := r.URL.Query().Get("cursor")
		switch cur {
		case "*":
			next := "c2"
			_ = json.NewEncoder(w).Encode(Results[Work]{
				Meta:    ResultsMeta{Count: 3, PerPage: 2, NextCursor: &next},
				Results: []Work{{ID: "W1"}, {ID: "W2"}},
			})
		case "c2":
			_ = json.NewEncoder(w).Encode(Results[Work]{
				Meta:    ResultsMeta{Count: 3, PerPage: 2},
				Results: []Work{{ID: "W3"}},
			})
		default:
			t.Errorf("unexpected cursor %q", cur)
		}
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL

	var collected []string
	err := c.IterateWorks(context.Background(), SearchOpts{Search: "x"}, func(page []Work) error {
		for _, w := range page {
			collected = append(collected, w.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(collected, ","); got != "W1,W2,W3" {
		t.Errorf("collected = %q", got)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

func TestClient_IterateWorks_callbackErrorStops(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		next := "c2"
		_ = json.NewEncoder(w).Encode(Results[Work]{
			Meta:    ResultsMeta{NextCursor: &next},
			Results: []Work{{ID: "W1"}},
		})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	wantErr := errSentinel{}
	err := c.IterateWorks(context.Background(), SearchOpts{}, func(_ []Work) error { return wantErr })
	if err != wantErr {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (callback error must halt)", hits)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "stop" }

func TestClient_SearchAuthors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/authors" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(Results[Author]{
			Meta:    ResultsMeta{Count: 1, PerPage: 25},
			Results: []Author{{ID: "A1", DisplayName: "Vaswani"}},
		})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	res, err := c.SearchAuthors(context.Background(), SearchOpts{Search: "vaswani"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].DisplayName != "Vaswani" {
		t.Errorf("got %+v", res)
	}
}
