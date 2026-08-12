package crossref

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// gweonWork is a real /works/{doi} response shape, trimmed — the Phil
// Trans B issue paper whose Zotero item arrived as a bare `document` with
// no venue and no creators (the repair that motivated this endpoint).
const gweonWork = `{"message":{
 "DOI":"10.1098/RSTB.2022.0048","type":"journal-article",
 "title":["Socially intelligent machines that learn from humans and help humans learn"],
 "container-title":["Philosophical Transactions of the Royal Society B"],
 "short-container-title":["Phil Trans R Soc B"],
 "author":[
  {"given":"Hyowon","family":"Gweon","sequence":"first"},
  {"given":"Judith","family":"Fan","sequence":"additional"},
  {"given":"Been","family":"Kim","sequence":"additional"}],
 "editor":[{"given":"Frank","family":"Editor","sequence":"first"}],
 "issued":{"date-parts":[[2023,6,5]]},
 "volume":"381","issue":"2251","page":"20220048",
 "publisher":"The Royal Society"}}`

// biorxivWork is the posted-content shape: no container-title; the
// repository name arrives in institution[].name, the subject area in
// group-title, and the preprint flag in subtype.
const biorxivWork = `{"message":{
 "DOI":"10.1101/2024.01.01.573000","type":"posted-content","subtype":"preprint",
 "title":["A very reproducible preprint"],
 "institution":[{"name":"bioRxiv"}],
 "group-title":"Neuroscience",
 "author":[{"given":"Ada","family":"Lovelace","sequence":"first"}],
 "issued":{"date-parts":[[2024,1,1]]}}}`

func TestWorkDecodesTheDossierFields(t *testing.T) {
	t.Parallel()
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(gweonWork))
	})

	rec, err := c.Work(context.Background(), "10.1098/rstb.2022.0048")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("want a record, got nil")
	}
	// The DOI goes into the URL path percent-encoded, slash included —
	// Crossref accepts both, and encoding is the spelling that cannot be
	// misread as extra path segments.
	if !strings.Contains(gotPath, "/works/10.1098%2Frstb.2022.0048") {
		t.Errorf("request path = %q, want percent-encoded DOI", gotPath)
	}
	if rec.DOI != "10.1098/rstb.2022.0048" {
		t.Errorf("DOI = %q, want lowercased bare form", rec.DOI)
	}
	if rec.Type != "journal-article" || rec.Venue != "Philosophical Transactions of the Royal Society B" {
		t.Errorf("type/venue = %q/%q", rec.Type, rec.Venue)
	}
	if rec.Year != 2023 || rec.Volume != "381" || rec.Issue != "2251" || rec.Pages != "20220048" {
		t.Errorf("year/vol/issue/pages = %d/%q/%q/%q", rec.Year, rec.Volume, rec.Issue, rec.Pages)
	}
	if rec.Publisher != "The Royal Society" {
		t.Errorf("publisher = %q", rec.Publisher)
	}
	wantAuthors := []Contributor{
		{Given: "Hyowon", Family: "Gweon", Sequence: "first"},
		{Given: "Judith", Family: "Fan", Sequence: "additional"},
		{Given: "Been", Family: "Kim", Sequence: "additional"},
	}
	if len(rec.Authors) != 3 {
		t.Fatalf("authors = %+v, want 3", rec.Authors)
	}
	for i, w := range wantAuthors {
		if rec.Authors[i] != w {
			t.Errorf("author[%d] = %+v, want %+v", i, rec.Authors[i], w)
		}
	}
	if len(rec.Editors) != 1 || rec.Editors[0].Family != "Editor" {
		t.Errorf("editors = %+v", rec.Editors)
	}
}

func TestWorkDecodesPostedContent(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(biorxivWork))
	})

	rec, err := c.Work(context.Background(), "10.1101/2024.01.01.573000")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Type != "posted-content" || rec.Subtype != "preprint" {
		t.Errorf("type/subtype = %q/%q", rec.Type, rec.Subtype)
	}
	if len(rec.Institutions) != 1 || rec.Institutions[0] != "bioRxiv" {
		t.Errorf("institutions = %+v, want [bioRxiv]", rec.Institutions)
	}
	if rec.GroupTitle != "Neuroscience" {
		t.Errorf("group-title = %q", rec.GroupTitle)
	}
	if rec.Venue != "" {
		t.Errorf("venue = %q, posted-content has no container-title", rec.Venue)
	}
}

func TestWorkNotFoundIsAnAnswerNotAnError(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Resource not found.", http.StatusNotFound)
	})

	rec, err := c.Work(context.Background(), "10.9999/nope")
	if err != nil {
		t.Fatalf("a 404 is Crossref's honest 'no record', not a failure: %v", err)
	}
	if rec != nil {
		t.Errorf("rec = %+v, want nil", rec)
	}
}

func TestWorkServerErrorIsAnError(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	if _, err := c.Work(context.Background(), "10.1000/x"); err == nil {
		t.Fatal("a 500 must surface as an error, never as 'no record'")
	}
}
