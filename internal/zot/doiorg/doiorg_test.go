package doiorg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const careyCSL = `{
  "DOI": "10.1093/acprof:oso/9780195367638.001.0001",
  "type": "monograph",
  "title": "The Origin of Concepts",
  "publisher": "Oxford University Press",
  "issued": {"date-parts": [[2009, 4, 2]]}
}`

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New()
	c.BaseURL = srv.URL
	return c
}

func TestResolve_MapsCSLJSON(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Content negotiation is the whole mechanism — without this header
		// doi.org 302s to the publisher's landing page instead.
		if got := r.Header.Get("Accept"); !strings.Contains(got, "citationstyles.csl+json") {
			t.Errorf("Accept = %q", got)
		}
		if r.URL.Path != "/10.1093/acprof:oso/9780195367638.001.0001" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(careyCSL))
	})

	got, err := c.Resolve(context.Background(), "10.1093/acprof:oso/9780195367638.001.0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Origin of Concepts" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Year != 2009 {
		t.Errorf("year = %d", got.Year)
	}
	if got.Type != "monograph" {
		t.Errorf("type = %q", got.Type)
	}
	if got.DOI != "10.1093/acprof:oso/9780195367638.001.0001" {
		t.Errorf("doi = %q", got.DOI)
	}
}

// TestResolve_VenueFromContainerTitle — journal articles carry the venue in
// container-title; a monograph carries a publisher instead.
func TestResolve_VenueFromContainerTitle(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"DOI":"10.1/x","title":"A Paper",
		  "container-title":"Trends in Cognitive Sciences",
		  "issued":{"date-parts":[[2007]]}}`))
	})
	got, err := c.Resolve(context.Background(), "10.1/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Venue != "Trends in Cognitive Sciences" {
		t.Errorf("venue = %q", got.Venue)
	}
}

func TestResolve_PublisherIsTheVenueFallback(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(careyCSL))
	})
	got, err := c.Resolve(context.Background(), "10.1/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Venue != "Oxford University Press" {
		t.Errorf("venue = %q", got.Venue)
	}
}

// TestResolve_TitleCanBeAnArray — some registrants (DataCite especially)
// serialize `title` as a list. Dropping the title would make the record
// useless as evidence that the work is real.
func TestResolve_TitleCanBeAnArray(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"DOI":"10.48550/arXiv.1801.00173",
		  "title":["Theory of Deep Learning III"],"issued":{"date-parts":[[2018]]}}`))
	})
	got, err := c.Resolve(context.Background(), "10.48550/arXiv.1801.00173")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Theory of Deep Learning III" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestResolve_404IsErrNotFound(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "DOI Not Found", http.StatusNotFound)
	})
	if _, err := c.Resolve(context.Background(), "10.1234/invented"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestResolve_ServerErrorIsNotNotFound — doi.org being down says nothing
// about whether the DOI exists.
func TestResolve_ServerErrorIsNotNotFound(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	})
	_, err := c.Resolve(context.Background(), "10.1/x")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want a non-ErrNotFound error", err)
	}
}

func TestResolve_StripsDOIPrefixes(t *testing.T) {
	t.Parallel()
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"DOI":"10.1/x","title":"t"}`))
	})
	for _, in := range []string{
		"10.1/x", "doi:10.1/x", "https://doi.org/10.1/x", "http://dx.doi.org/10.1/x",
	} {
		if _, err := c.Resolve(context.Background(), in); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if gotPath != "/10.1/x" {
			t.Errorf("%s → path %q, want /10.1/x", in, gotPath)
		}
	}
}

func TestArxivDOI(t *testing.T) {
	t.Parallel()
	if got := ArxivDOI("1801.00173"); got != "10.48550/arXiv.1801.00173" {
		t.Errorf("ArxivDOI() = %q", got)
	}
	// A version suffix isn't part of the registered DOI.
	if got := ArxivDOI("1706.03762v5"); got != "10.48550/arXiv.1706.03762" {
		t.Errorf("ArxivDOI() = %q", got)
	}
}
