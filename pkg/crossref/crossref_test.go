package crossref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serialPosition is a real response shape, trimmed. The query was "The
// serial position effect of free recall" (Murdock 1962) and Crossref's
// TOP HIT is a different work with a similar title — a PsycEXTRA dataset.
// The correct record is second. This is the whole reason Search returns
// candidates instead of an answer.
const serialPosition = `{"message":{"total-results":2,"items":[
 {"DOI":"10.1037/e465422008-008","type":"dataset",
  "title":["Serial and Free Recall: Number and Form of Serial Position Functions"],
  "container-title":["PsycEXTRA Dataset"],"issued":{"date-parts":[[2008]]}},
 {"DOI":"10.1037/h0045106","type":"journal-article",
  "title":["The serial position effect of free recall"],
  "container-title":["Journal of Experimental Psychology"],"issued":{"date-parts":[[1962]]}}
]}}`

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("someone@example.org")
	c.BaseURL = srv.URL
	return c
}

func TestSearchReturnsEveryCandidateInOrder(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(serialPosition))
	})

	got, err := c.Search(context.Background(), "The serial position effect of free recall", 8)
	if err != nil {
		t.Fatal(err)
	}
	// Returning only the top hit would hand back the PsycEXTRA dataset and
	// call it Murdock 1962. Crossref ranks fuzzily; deciding which
	// candidate IS the item needs title_norm, which lives in zot. So this
	// package fetches and never chooses.
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want both", len(got))
	}
	if got[0].DOI != "10.1037/e465422008-008" || got[1].DOI != "10.1037/h0045106" {
		t.Errorf("candidates reordered: %+v", got)
	}
	if got[1].Year != 1962 || got[1].Venue != "Journal of Experimental Psychology" {
		t.Errorf("second candidate = %+v", got[1])
	}
	if got[1].Title != "The serial position effect of free recall" {
		t.Errorf("title = %q", got[1].Title)
	}
}

func TestSearchIsPolite(t *testing.T) {
	t.Parallel()
	var gotMailto, gotUA, gotRows string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMailto = r.URL.Query().Get("mailto")
		gotRows = r.URL.Query().Get("rows")
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"message":{"items":[]}}`))
	})
	if _, err := c.Search(context.Background(), "anything", 5); err != nil {
		t.Fatal(err)
	}
	// The polite pool is the difference between a sweep that finishes and
	// one that gets rate-limited halfway. Crossref asks for a contact in
	// both places and honours either; sending both costs nothing.
	if gotMailto != "someone@example.org" {
		t.Errorf("mailto = %q", gotMailto)
	}
	if !strings.Contains(gotUA, "someone@example.org") {
		t.Errorf("User-Agent = %q, want the contact in it", gotUA)
	}
	if gotRows != "5" {
		t.Errorf("rows = %q, want the cap to reach the wire", gotRows)
	}
}

func TestNoMatchIsNotAnError(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"total-results":0,"items":[]}}`))
	})
	got, err := c.Search(context.Background(), "a title nobody registered", 5)
	// "Crossref has no exact match" is the ordinary answer for a preprint,
	// a 1947 paper, or a book chapter — a third of this library. Making it
	// an error would turn the sweep into a cascade of failures and, worse,
	// would let a transport fault masquerade as a negative verdict.
	if err != nil {
		t.Fatalf("empty result errored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d candidates from an empty response", len(got))
	}
}

func TestATransportFailureIsNeverAVerdict(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	got, err := c.Search(context.Background(), "anything", 5)
	if err == nil {
		t.Fatal("a 503 returned no error — it would read as 'no such paper'")
	}
	if got != nil {
		t.Errorf("got %v candidates alongside an error", len(got))
	}
}

func TestATitleIsAPhraseNotABagOfWords(t *testing.T) {
	t.Parallel()
	var gotQuery string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query.bibliographic")
		_, _ = w.Write([]byte(`{"message":{"items":[]}}`))
	})
	title := `Emotion, Development, and Self-Organization: "Dynamic Systems"`
	if _, err := c.Search(context.Background(), title, 5); err != nil {
		t.Fatal(err)
	}
	// Unlike OpenAlex's filter DSL, Crossref's query params carry no
	// metacharacters — commas and colons are literal here. The title must
	// therefore arrive INTACT, since mangling it to dodge a problem this
	// API does not have would only lose matches.
	if gotQuery != title {
		t.Errorf("query.bibliographic = %q, want the title verbatim", gotQuery)
	}
}
