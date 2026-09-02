package doiorg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sciminds/sci/internal/zot/csl"
)

const careyCSL = `{
  "DOI": "10.1093/acprof:oso/9780195367638.001.0001",
  "type": "monograph",
  "title": "The Origin of Concepts",
  "publisher": "Oxford University Press",
  "author": [{"family": "Carey", "given": "Susan"}],
  "issued": {"date-parts": [[2009, 4, 2]]}
}`

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTestServer(t, h)
	srv.Start()
	c := New()
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	return c
}

func TestResolve_NegotiatesCSLAndMapsRecord(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Content negotiation is the whole mechanism — without this header
		// doi.org 302s to the publisher's landing page instead.
		if got := r.Header.Get("Accept"); !strings.Contains(got, "citationstyles.csl+json") {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "sci (") {
			t.Errorf("User-Agent = %q, want sci's", got)
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
	if got.Title != "The Origin of Concepts" || got.Type != "monograph" || got.Publisher != "Oxford University Press" {
		t.Errorf("record = %+v", got)
	}
	if got.Issued != (csl.Date{Year: 2009, Month: 4, Day: 2}) {
		t.Errorf("issued = %+v, want 2009-04-02", got.Issued)
	}
	if len(got.Authors) != 1 || got.Authors[0].Family != "Carey" {
		t.Errorf("authors = %+v, want Carey", got.Authors)
	}
}

// TestResolve_TitleCanBeAList — DataCite serializes title as a list;
// dropping it would make the record useless as evidence the work exists.
func TestResolve_TitleCanBeAList(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"DOI":"10.48550/arXiv.1801.00173",
		  "title":["Theory of Deep Learning III"],"container-title":["arXiv"],
		  "issued":{"date-parts":[[2018]]}}`))
	})
	got, err := c.Resolve(context.Background(), "10.48550/arXiv.1801.00173")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Theory of Deep Learning III" || got.ContainerTitle != "arXiv" {
		t.Errorf("record = %+v", got)
	}
	if got.Issued != (csl.Date{Year: 2018}) {
		t.Errorf("issued = %+v, want year-only 2018", got.Issued)
	}
}

func TestResolve_DatePartsOfTwo(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"DOI":"10.1/x","title":"t","issued":{"date-parts":[[2007,11]]}}`))
	})
	got, err := c.Resolve(context.Background(), "10.1/x")
	if err != nil {
		t.Fatal(err)
	}
	if got.Issued != (csl.Date{Year: 2007, Month: 11}) {
		t.Errorf("issued = %+v, want 2007-11", got.Issued)
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
// about whether the DOI exists, so the two must stay distinguishable.
func TestResolve_ServerErrorIsNotNotFound(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.Resolve(context.Background(), "10.1/x")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a non-ErrNotFound error", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want the status in the message", err)
	}
}

func TestResolve_NormalizesEveryCitedForm(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"10.1/x", "doi:10.1/x", "DOI:10.1/x", " https://doi.org/10.1/x ", "http://dx.doi.org/10.1/x",
	} {
		var gotPath string
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"DOI":"10.1/x","title":"t"}`))
		})
		if _, err := c.Resolve(context.Background(), in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if gotPath != "/10.1/x" {
			t.Errorf("%q → path %q, want /10.1/x", in, gotPath)
		}
	}
}

// TestResolve_EscapesReservedCharactersInDOI — a DOI may carry `#`, `?`
// and `%` (Wiley SICIs, some legacy registrants). Concatenated raw into
// the URL, `#` and `?` would truncate the path and `%` would corrupt it,
// and a truncated DOI resolves to the WRONG record rather than failing.
func TestResolve_EscapesReservedCharactersInDOI(t *testing.T) {
	t.Parallel()
	const id = "10.1002/(SICI)1097-0258#a?b%c"
	var gotPath, gotQuery, gotFragment string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotFragment = r.URL.Path, r.URL.RawQuery, r.URL.Fragment
		_, _ = w.Write([]byte(`{"DOI":"10.1/x","title":"t"}`))
	})
	if _, err := c.Resolve(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/"+id {
		t.Errorf("server saw path %q, want %q", gotPath, "/"+id)
	}
	if gotQuery != "" || gotFragment != "" {
		t.Errorf("server saw query %q / fragment %q, want the whole DOI in the path", gotQuery, gotFragment)
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"10.1/x":                     "10.1/x",
		"doi:10.1/x":                 "10.1/x",
		"https://doi.org/10.1/x":     "10.1/x",
		"HTTPS://DX.DOI.ORG/10.1/x":  "10.1/x",
		"  10.1/x\n":                 "10.1/x",
		"https://example.org/10.1/x": "https://example.org/10.1/x",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArxivDOI(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"1801.00173":       "10.48550/arXiv.1801.00173",
		"1706.03762v5":     "10.48550/arXiv.1706.03762", // one DOI per paper, not per version
		"hep-th/9901001v2": "10.48550/arXiv.hep-th/9901001",
	}
	for in, want := range cases {
		if got := ArxivDOI(in); got != want {
			t.Errorf("ArxivDOI(%q) = %q, want %q", in, got, want)
		}
	}
}
