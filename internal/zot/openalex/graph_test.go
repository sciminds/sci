package openalex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// graphTestServer stands up an httptest server that records the incoming
// request URLs and replies with a fixed Results[Work] payload. Lets us
// assert filter/sort/per_page wiring without hitting OpenAlex.
func graphTestServer(t *testing.T, payload Results[Work]) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	c := NewClient("", "")
	c.BaseURL = srv.URL
	return c, &seen
}

func TestCitedBy_BuildsFilterAndDefaults(t *testing.T) {
	t.Parallel()
	c, seen := graphTestServer(t, Results[Work]{Results: []Work{{ID: "https://openalex.org/W1"}}})
	res, err := c.CitedBy(context.Background(), "W3105657479", SearchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 {
		t.Errorf("results len = %d, want 1", len(res.Results))
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %d, want 1", len(*seen))
	}
	got := (*seen)[0]
	for _, fragment := range []string{
		"filter=cited_by%3AW3105657479",
		"per_page=25",
		"sort=cited_by_count%3Adesc",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("missing %q in %s", fragment, got)
		}
	}
}

func TestCitedBy_PreservesUserFilters(t *testing.T) {
	t.Parallel()
	c, seen := graphTestServer(t, Results[Work]{})
	_, err := c.CitedBy(context.Background(), "W123", SearchOpts{
		Filter: map[string]string{"from_publication_date": "2020-01-01"},
		Sort:   "publication_date:desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	// Both filters survive; sort override wins over default.
	if !strings.Contains(got, "cited_by") || !strings.Contains(got, "from_publication_date") {
		t.Errorf("filters not preserved: %s", got)
	}
	if !strings.Contains(got, "sort=publication_date%3Adesc") {
		t.Errorf("custom sort overridden: %s", got)
	}
}

func TestWorksByID_BatchesRequests(t *testing.T) {
	t.Parallel()
	c, seen := graphTestServer(t, Results[Work]{Results: []Work{{ID: "x"}}})
	// 110 ids should produce 3 batched requests at the 50-id default.
	ids := make([]string, 110)
	for i := range ids {
		ids[i] = "W" + strings.Repeat("1", i+1)
	}
	_, err := c.WorksByID(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(*seen); got != 3 {
		t.Fatalf("requests = %d, want 3 batched", got)
	}
}

func TestWorksByID_EmptySkipsHTTP(t *testing.T) {
	t.Parallel()
	c, seen := graphTestServer(t, Results[Work]{})
	got, err := c.WorksByID(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil for empty input", got)
	}
	if len(*seen) != 0 {
		t.Errorf("HTTP requests = %d, want 0", len(*seen))
	}
}

func TestShortIDFromAny(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"W12345":                      "W12345",
		"https://openalex.org/W12345": "W12345",
		"http://openalex.org/w12345":  "W12345",
		"  W123  ":                    "W123",
		"":                            "",
	}
	for in, want := range cases {
		if got := shortIDFromAny(in); got != want {
			t.Errorf("shortIDFromAny(%q) = %q, want %q", in, got, want)
		}
	}
}
