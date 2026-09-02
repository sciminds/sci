package csl

import (
	"reflect"
	"testing"
)

func TestParse_StringTitle(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{
	  "DOI": "10.1093/acprof:oso/9780195367638.001.0001",
	  "type": "monograph",
	  "title": "The Origin of Concepts",
	  "publisher": "Oxford University Press",
	  "ISBN": "9780195367638",
	  "issued": {"date-parts": [[2009, 4, 2]]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := &Record{
		DOI: "10.1093/acprof:oso/9780195367638.001.0001", Type: "monograph",
		Title: "The Origin of Concepts", Publisher: "Oxford University Press", ISBN: "9780195367638",
		Issued: Date{Year: 2009, Month: 4, Day: 2},
	}
	if !reflect.DeepEqual(rec, want) {
		t.Errorf("Parse = %+v, want %+v", rec, want)
	}
}

// TestParse_ListValuedFields — DataCite serializes title, container-title
// and ISSN as lists; the first entry is the value.
func TestParse_ListValuedFields(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{
	  "DOI": "10.48550/arXiv.1801.00173",
	  "title": ["Theory of Deep Learning III", "Alternate"],
	  "container-title": ["arXiv"],
	  "ISSN": ["1234-5678", "8765-4321"],
	  "issued": {"date-parts": [[2018]]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Title != "Theory of Deep Learning III" || rec.ContainerTitle != "arXiv" || rec.ISSN != "1234-5678" {
		t.Errorf("Parse = %+v, want first entries of the list-valued fields", rec)
	}
}

// TestParse_LowercasesDOI — DOIs are case-insensitive by spec and DataCite
// serves them in upper case; one canonical form keeps comparisons honest.
func TestParse_LowercasesDOI(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{"DOI":" 10.48550/ARXIV.1706.03762 ","title":"t"}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.DOI != "10.48550/arxiv.1706.03762" {
		t.Errorf("DOI = %q, want lower-cased and trimmed", rec.DOI)
	}
}

// TestParse_NonNumericDatePartReadsAsUnknown — Crossref emits season
// strings ([[2019,"Spring"]]) for some journals; the year must survive.
func TestParse_NonNumericDatePartReadsAsUnknown(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{"DOI":"10.1/x","issued":{"date-parts":[[2019,"Spring"]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Issued != (Date{Year: 2019}) {
		t.Errorf("Issued = %+v, want year 2019 with the season dropped", rec.Issued)
	}
}

// dataciteArxivCSL is the shape doi.org serves for a DataCite arXiv
// record (10.48550/arXiv.1706.03762), verified live: type "article", DOI
// in upper case, publisher "arXiv", no container-title.
const dataciteArxivCSL = `{
  "type": "article",
  "id": "https://doi.org/10.48550/arxiv.1706.03762",
  "categories": ["Computation and Language (cs.CL)", "FOS: Computer and information sciences"],
  "author": [{"family": "Vaswani", "given": "Ashish"}, {"family": "Shazeer", "given": "Noam"}],
  "issued": {"date-parts": [[2017]]},
  "abstract": "The dominant sequence transduction models…",
  "DOI": "10.48550/ARXIV.1706.03762",
  "publisher": "arXiv",
  "title": "Attention Is All You Need",
  "URL": "https://arxiv.org/abs/1706.03762",
  "copyright": "arXiv.org perpetual, non-exclusive license",
  "version": "5"
}`

func TestRecordItemType_ArxivArticleIsPreprint(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(dataciteArxivCSL))
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.ItemType(); got != "preprint" {
		t.Errorf("ItemType() = %q, want preprint for a DataCite arXiv record", got)
	}
	// The plain table still says journalArticle for a bare "article".
	if got := ItemType("article"); got != "journalArticle" {
		t.Errorf("ItemType(article) = %q, want journalArticle", got)
	}
	plain := &Record{Type: "article", DOI: "10.1000/x"}
	if got := plain.ItemType(); got != "journalArticle" {
		t.Errorf("non-arXiv article ItemType() = %q, want journalArticle", got)
	}
	// Publisher alone also marks a preprint, when the DOI is not an arXiv one.
	viaPublisher := &Record{Type: "article", DOI: "10.1000/x", Publisher: "arXiv"}
	if got := viaPublisher.ItemType(); got != "preprint" {
		t.Errorf("publisher=arXiv ItemType() = %q, want preprint", got)
	}
}

func TestToItem_ArxivPreprintCarriesRepositoryAndArchiveID(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(dataciteArxivCSL))
	if err != nil {
		t.Fatal(err)
	}
	preprintFields := []string{"title", "abstractNote", "repository", "archiveID", "date", "DOI", "url", "extra"}
	got := ToItem(rec, preprintFields, []string{"author"})
	want := map[string]any{
		"itemType":     "preprint",
		"title":        "Attention Is All You Need",
		"abstractNote": "The dominant sequence transduction models…",
		"repository":   "arXiv",
		"archiveID":    "arXiv:1706.03762",
		"date":         "2017",
		"DOI":          "10.48550/arxiv.1706.03762",
		"url":          "https://arxiv.org/abs/1706.03762",
		"creators": []map[string]any{
			{"creatorType": "author", "firstName": "Ashish", "lastName": "Vaswani"},
			{"creatorType": "author", "firstName": "Noam", "lastName": "Shazeer"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem =\n%#v\nwant\n%#v", got, want)
	}
}

// TestToItem_ArxivFieldsOnlyWhenDeclared — repository/archiveID are never
// forced onto a field list that lacks them, and archiveID needs an arXiv
// DOI to derive the id from.
func TestToItem_ArxivFieldsOnlyWhenDeclared(t *testing.T) {
	t.Parallel()
	rec := &Record{Type: "article", DOI: "10.48550/arxiv.hep-th/9901001", Publisher: "arXiv", Title: "T"}
	got := ToItem(rec, []string{"title"}, nil)
	want := map[string]any{"itemType": "preprint", "title": "T"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem = %#v, want %#v", got, want)
	}
	got = ToItem(rec, []string{"title", "repository", "archiveID"}, nil)
	if got["repository"] != "arXiv" || got["archiveID"] != "arXiv:hep-th/9901001" {
		t.Errorf("ToItem = %#v, want repository arXiv and archiveID arXiv:hep-th/9901001", got)
	}
	// Marked arXiv by publisher only: no id to derive, so no archiveID.
	byPublisher := &Record{Type: "article", DOI: "10.1000/x", Publisher: "arXiv", Title: "T"}
	got = ToItem(byPublisher, []string{"title", "repository", "archiveID"}, nil)
	if got["repository"] != "arXiv" {
		t.Errorf("repository = %v, want arXiv", got["repository"])
	}
	if _, ok := got["archiveID"]; ok {
		t.Errorf("archiveID = %v, want absent when the DOI is not an arXiv one", got["archiveID"])
	}
}

// TestParse_NumericVolume — some registrants send volume/issue as JSON
// numbers; Zotero wants strings either way.
func TestParse_NumericVolume(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{"DOI":"10.1/x","title":"t","volume":12,"issue":"3","page":"100-110"}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Volume != "12" || rec.Issue != "3" || rec.Page != "100-110" {
		t.Errorf("Parse = %+v, want volume 12, issue 3, page 100-110", rec)
	}
}

func TestParse_DateParts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Date
	}{
		{`[[2007]]`, Date{Year: 2007}},
		{`[[2007, 11]]`, Date{Year: 2007, Month: 11}},
		{`[[2007, 11, 5]]`, Date{Year: 2007, Month: 11, Day: 5}},
		{`[]`, Date{}},
	}
	for _, tc := range cases {
		rec, err := Parse([]byte(`{"DOI":"10.1/x","issued":{"date-parts":` + tc.in + `}}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if rec.Issued != tc.want {
			t.Errorf("date-parts %s → %+v, want %+v", tc.in, rec.Issued, tc.want)
		}
	}
}

func TestParse_MissingIssuedIsZeroDate(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{"DOI":"10.1/x","title":"t"}`))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Issued != (Date{}) {
		t.Errorf("Issued = %+v, want zero", rec.Issued)
	}
}

func TestParse_Names(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte(`{
	  "DOI": "10.1/x",
	  "author": [{"family":"Carey","given":"Susan"},{"literal":"NASA"}],
	  "editor": [{"family":"Smith","given":"Jo"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	wantAuthors := []Name{{Family: "Carey", Given: "Susan"}, {Literal: "NASA"}}
	if !reflect.DeepEqual(rec.Authors, wantAuthors) {
		t.Errorf("Authors = %+v, want %+v", rec.Authors, wantAuthors)
	}
	wantEditors := []Name{{Family: "Smith", Given: "Jo"}}
	if !reflect.DeepEqual(rec.Editors, wantEditors) {
		t.Errorf("Editors = %+v, want %+v", rec.Editors, wantEditors)
	}
}

func TestParse_RejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"DOI":`)); err == nil {
		t.Fatal("err = nil, want a decode error")
	}
}

func TestDateISO(t *testing.T) {
	t.Parallel()
	cases := map[Date]string{
		{}:                             "",
		{Year: 2009}:                   "2009",
		{Year: 2009, Month: 4}:         "2009-04",
		{Year: 2009, Month: 4, Day: 2}: "2009-04-02",
		{Year: 2009, Day: 2}:           "2009", // a day without a month is noise
	}
	for d, want := range cases {
		if got := d.ISO(); got != want {
			t.Errorf("%+v.ISO() = %q, want %q", d, got, want)
		}
	}
}

func TestItemType(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"article-journal":   "journalArticle",
		"article-magazine":  "magazineArticle",
		"article-newspaper": "newspaperArticle",
		"book":              "book",
		"monograph":         "book",
		"chapter":           "bookSection",
		"paper-conference":  "conferencePaper",
		"thesis":            "thesis",
		"report":            "report",
		"posted-content":    "preprint",
		"dataset":           "dataset",
		"webpage":           "webpage",
		"article":           "journalArticle",
		"":                  "document",
		"something-new":     "document",
	}
	for in, want := range cases {
		if got := ItemType(in); got != want {
			t.Errorf("ItemType(%q) = %q, want %q", in, got, want)
		}
	}
}

// journalFields is the field list Zotero reports for journalArticle, cut
// down to what these tests exercise.
var journalFields = []string{"title", "abstractNote", "publicationTitle", "volume", "issue", "pages", "date", "DOI", "ISSN", "url", "extra"}

var authorEditor = []string{"author", "editor", "contributor"}

func TestToItem_JournalArticle(t *testing.T) {
	t.Parallel()
	rec := &Record{
		DOI: "10.1016/j.tics.2007.01.001", Type: "article-journal",
		Title: "Mentalizing", Abstract: "A review.", ContainerTitle: "Trends in Cognitive Sciences",
		Volume: "11", Issue: "2", Page: "77-83", ISSN: "1364-6613", URL: "https://doi.org/10.1016/j.tics.2007.01.001",
		Publisher: "Elsevier",
		Authors:   []Name{{Family: "Frith", Given: "Chris D."}, {Literal: "The Consortium"}},
		Editors:   []Name{{Family: "Ed", Given: "Itor"}},
		Issued:    Date{Year: 2007, Month: 2},
	}
	got := ToItem(rec, journalFields, authorEditor)
	want := map[string]any{
		"itemType":         "journalArticle",
		"title":            "Mentalizing",
		"abstractNote":     "A review.",
		"publicationTitle": "Trends in Cognitive Sciences",
		"volume":           "11",
		"issue":            "2",
		"pages":            "77-83",
		"ISSN":             "1364-6613",
		"url":              "https://doi.org/10.1016/j.tics.2007.01.001",
		"DOI":              "10.1016/j.tics.2007.01.001",
		"date":             "2007-02",
		"creators": []map[string]any{
			{"creatorType": "author", "firstName": "Chris D.", "lastName": "Frith"},
			{"creatorType": "author", "name": "The Consortium"},
			{"creatorType": "editor", "firstName": "Itor", "lastName": "Ed"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem =\n%#v\nwant\n%#v", got, want)
	}
	if _, ok := got["publisher"]; ok {
		t.Error("publisher was sent although journalArticle has no publisher field")
	}
}

// TestToItem_BookPutsDOIInExtra — Zotero's book type has no DOI field, so
// the DOI rides in extra the way Zotero itself stores it.
func TestToItem_BookPutsDOIInExtra(t *testing.T) {
	t.Parallel()
	rec := &Record{
		DOI: "10.1093/acprof:oso/9780195367638.001.0001", Type: "monograph",
		Title: "The Origin of Concepts", Publisher: "Oxford University Press", ISBN: "9780195367638",
		Authors: []Name{{Family: "Carey", Given: "Susan"}},
		Issued:  Date{Year: 2009, Month: 4, Day: 2},
	}
	bookFields := []string{"title", "publisher", "ISBN", "date", "extra", "numPages"}
	got := ToItem(rec, bookFields, authorEditor)
	want := map[string]any{
		"itemType":  "book",
		"title":     "The Origin of Concepts",
		"publisher": "Oxford University Press",
		"ISBN":      "9780195367638",
		"date":      "2009-04-02",
		"extra":     "DOI: 10.1093/acprof:oso/9780195367638.001.0001",
		"creators": []map[string]any{
			{"creatorType": "author", "firstName": "Susan", "lastName": "Carey"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem =\n%#v\nwant\n%#v", got, want)
	}
}

func TestToItem_VenueFallsBackToBookTitleThenProceedingsTitle(t *testing.T) {
	t.Parallel()
	rec := &Record{Type: "chapter", Title: "Ch", ContainerTitle: "The Handbook"}
	got := ToItem(rec, []string{"title", "bookTitle"}, nil)
	if got["bookTitle"] != "The Handbook" {
		t.Errorf("chapter: bookTitle = %v, want The Handbook", got["bookTitle"])
	}
	rec = &Record{Type: "paper-conference", Title: "P", ContainerTitle: "Proc. CogSci"}
	got = ToItem(rec, []string{"title", "proceedingsTitle"}, nil)
	if got["proceedingsTitle"] != "Proc. CogSci" {
		t.Errorf("paper-conference: proceedingsTitle = %v, want Proc. CogSci", got["proceedingsTitle"])
	}
}

// TestToItem_NoVenueFieldDropsContainerTitle — a report has none of the
// three venue fields; the container title is dropped rather than misfiled.
func TestToItem_NoVenueFieldDropsContainerTitle(t *testing.T) {
	t.Parallel()
	rec := &Record{Type: "report", Title: "R", ContainerTitle: "Some Series"}
	got := ToItem(rec, []string{"title", "reportNumber", "institution"}, nil)
	want := map[string]any{"itemType": "report", "title": "R"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem = %#v, want %#v", got, want)
	}
}

// TestToItem_EditorsOnlyWhenTypeTakesThem — a type whose creator types
// exclude editor gets no editor entries at all.
func TestToItem_EditorsOnlyWhenTypeTakesThem(t *testing.T) {
	t.Parallel()
	rec := &Record{Type: "posted-content", Title: "Pre",
		Authors: []Name{{Family: "A", Given: "B"}}, Editors: []Name{{Family: "E", Given: "F"}}}
	got := ToItem(rec, []string{"title"}, []string{"author"})
	want := []map[string]any{{"creatorType": "author", "firstName": "B", "lastName": "A"}}
	if !reflect.DeepEqual(got["creators"], want) {
		t.Errorf("creators = %#v, want %#v", got["creators"], want)
	}
	if got["itemType"] != "preprint" {
		t.Errorf("itemType = %v, want preprint", got["itemType"])
	}
}

func TestToItem_NoCreatorsOmitsKey(t *testing.T) {
	t.Parallel()
	got := ToItem(&Record{Type: "webpage", Title: "W"}, []string{"title"}, authorEditor)
	if _, ok := got["creators"]; ok {
		t.Errorf("creators = %v, want the key absent when there are none", got["creators"])
	}
}

// TestToItem_DOIDroppedWhenNeitherFieldNorExtra pins that a DOI is never
// smuggled into a field the type does not declare.
func TestToItem_DOIDroppedWhenNeitherFieldNorExtra(t *testing.T) {
	t.Parallel()
	got := ToItem(&Record{Type: "book", DOI: "10.1/x", Title: "B"}, []string{"title"}, nil)
	want := map[string]any{"itemType": "book", "title": "B"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToItem = %#v, want %#v", got, want)
	}
}
