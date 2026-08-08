package openalex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"openalex works id", "W4389428231", "W4389428231", false},
		{"openalex id lowercases to upper", "w4389428231", "W4389428231", false},
		{"openalex authors id", "A5061940714", "A5061940714", false},
		{"prefixed doi passes through", "doi:10.1038/s41586-020-2649-2", "doi:10.1038/s41586-020-2649-2", false},
		{"bare doi gets doi prefix", "10.1038/s41586-020-2649-2", "doi:10.1038/s41586-020-2649-2", false},
		{"doi.org url", "https://doi.org/10.1038/s41586-020-2649-2", "doi:10.1038/s41586-020-2649-2", false},
		{"dx.doi.org url", "http://dx.doi.org/10.1038/s41586-020-2649-2", "doi:10.1038/s41586-020-2649-2", false},
		{"arxiv id with prefix", "arXiv:2310.01234", "arxiv:2310.01234", false},
		{"bare arxiv id", "2310.01234", "arxiv:2310.01234", false},
		{"pmid prefixed", "pmid:12345678", "pmid:12345678", false},
		{"bare 8-digit numeric → pmid", "12345678", "pmid:12345678", false},
		{"whitespace trimmed", "  W1234567  ", "W1234567", false},
		{"empty", "", "", true},
		{"unrecognized", "not-a-real-id", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeID(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClient_ResolveWork(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DOI input should route to /works/doi:10.xxx/...
		if r.URL.Path != "/works/doi:10.1038/nature12373" {
			t.Errorf("path = %q", r.URL.Path)
		}
		title := "Lorem ipsum"
		_ = json.NewEncoder(w).Encode(Work{ID: "https://openalex.org/W42", Title: &title})
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.BaseURL = srv.URL
	work, err := c.ResolveWork(context.Background(), "10.1038/nature12373")
	if err != nil {
		t.Fatal(err)
	}
	if work.Title == nil || *work.Title != "Lorem ipsum" {
		t.Errorf("title = %v", work.Title)
	}
}
