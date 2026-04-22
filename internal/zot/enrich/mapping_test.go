package enrich

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// strPtr is a tiny helper to keep the table tests below readable.
func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func TestToItemFields_journalArticle(t *testing.T) {
	t.Parallel()
	doi := "https://doi.org/10.1038/nature12373"
	title := "Genome sequencing"
	pubDate := "2013-09-25"
	journal := "Nature"
	landing := "https://www.nature.com/articles/nature12373"
	lang := "en"
	typ := "journal-article"

	w := &openalex.Work{
		ID:              "https://openalex.org/W2086842081",
		DOI:             &doi,
		Title:           &title,
		PublicationDate: &pubDate,
		Type:            &typ,
		Language:        &lang,
		PrimaryLocation: &openalex.Location{
			LandingPageURL: &landing,
			Source:         &openalex.SourceRef{DisplayName: journal, Type: strPtr("journal")},
		},
		Authorships: []openalex.Authorship{
			{Author: openalex.AuthorRef{DisplayName: "Alice Smith"}},
			{Author: openalex.AuthorRef{DisplayName: "Bob N. Jones"}},
		},
		AbstractInvertedIndex: map[string][]int{
			"The": {0}, "quick": {1}, "brown": {2}, "fox": {3},
		},
	}

	got := ToItemFields(w)

	if got.ItemType != client.JournalArticle {
		t.Errorf("ItemType = %q, want journalArticle", got.ItemType)
	}
	if got.Title == nil || *got.Title != title {
		t.Errorf("Title = %v", got.Title)
	}
	if got.DOI == nil || *got.DOI != "10.1038/nature12373" {
		t.Errorf("DOI = %v, want bare 10.xxx/yyy", got.DOI)
	}
	if got.Date == nil || *got.Date != pubDate {
		t.Errorf("Date = %v", got.Date)
	}
	if got.PublicationTitle == nil || *got.PublicationTitle != "Nature" {
		t.Errorf("PublicationTitle = %v", got.PublicationTitle)
	}
	if got.Url == nil || *got.Url != landing {
		t.Errorf("Url = %v", got.Url)
	}
	if got.Language == nil || *got.Language != "en" {
		t.Errorf("Language = %v", got.Language)
	}
	if got.Creators == nil || len(*got.Creators) != 2 {
		t.Fatalf("Creators = %v", got.Creators)
	}
	first := (*got.Creators)[0]
	if first.CreatorType != "author" || first.FirstName == nil || *first.FirstName != "Alice" || first.LastName == nil || *first.LastName != "Smith" {
		t.Errorf("first creator = %+v", first)
	}
	second := (*got.Creators)[1]
	if second.FirstName == nil || *second.FirstName != "Bob N." || second.LastName == nil || *second.LastName != "Jones" {
		t.Errorf("second creator = %+v", second)
	}
	if got.AbstractNote == nil || *got.AbstractNote != "The quick brown fox" {
		t.Errorf("AbstractNote = %v", got.AbstractNote)
	}
	if got.Extra == nil || !strings.Contains(*got.Extra, "OpenAlex: W2086842081") {
		t.Errorf("Extra must carry OpenAlex ID, got %v", got.Extra)
	}
}

func TestToItemFields_itemTypeMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		openalexType string
		want         client.ItemDataItemType
	}{
		{"journal-article", client.JournalArticle},
		{"article", client.JournalArticle},
		{"letter", client.JournalArticle},
		{"editorial", client.JournalArticle},
		{"review", client.JournalArticle},
		{"book", client.Book},
		{"monograph", client.Book},
		{"book-chapter", client.BookSection},
		{"dissertation", client.Thesis},
		{"preprint", client.Preprint},
		{"posted-content", client.Preprint},
		{"proceedings-article", client.ConferencePaper},
		{"report", client.Report},
		{"standard", client.Report},
		{"peer-review", client.Document},
		{"other", client.Document},
		{"", client.JournalArticle}, // sensible default when OpenAlex omits type
	}
	for _, tc := range cases {
		t.Run(tc.openalexType, func(t *testing.T) {
			var typ *string
			if tc.openalexType != "" {
				typ = &tc.openalexType
			}
			got := ToItemFields(&openalex.Work{ID: "https://openalex.org/W1", Type: typ})
			if got.ItemType != tc.want {
				t.Errorf("%q → %q, want %q", tc.openalexType, got.ItemType, tc.want)
			}
		})
	}
}

// TestToItemFields_sourceRoutingByType pins down that the primary-location
// source name lands in the type-appropriate Zotero field. Most importantly,
// preprint / thesis / report / book must NOT receive publicationTitle — Zotero
// rejects batch writes with "'publicationTitle' is not a valid field for type X"
// when we send it to non-journal items (OpenAlex Works with a Source.DisplayName
// are common for all of these types).
func TestToItemFields_sourceRoutingByType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		openalexType  string
		wantItemType  client.ItemDataItemType
		wantInField   string // one of: publicationTitle, proceedingsTitle, bookTitle, "" (must be unset)
		sourceDisplay string
	}{
		{"journal-article", client.JournalArticle, "publicationTitle", "Nature"},
		{"proceedings-article", client.ConferencePaper, "proceedingsTitle", "NeurIPS 2023"},
		{"book-chapter", client.BookSection, "bookTitle", "Handbook of RL"},
		{"preprint", client.Preprint, "", "bioRxiv"},
		{"posted-content", client.Preprint, "", "arXiv"},
		{"dissertation", client.Thesis, "", "MIT Archive"},
		{"report", client.Report, "", "NIST"},
		{"book", client.Book, "", "Cambridge Univ Press"},
	}
	for _, tc := range cases {
		t.Run(tc.openalexType, func(t *testing.T) {
			typ := tc.openalexType
			got := ToItemFields(&openalex.Work{
				ID:   "https://openalex.org/W1",
				Type: &typ,
				PrimaryLocation: &openalex.Location{
					Source: &openalex.SourceRef{DisplayName: tc.sourceDisplay, Type: strPtr("journal")},
				},
			})
			if got.ItemType != tc.wantItemType {
				t.Fatalf("ItemType = %q, want %q", got.ItemType, tc.wantItemType)
			}
			// publicationTitle must be set only for journalArticle.
			gotPub := derefOr(got.PublicationTitle)
			gotProc := derefOr(got.ProceedingsTitle)
			gotBook := derefOr(got.BookTitle)
			switch tc.wantInField {
			case "publicationTitle":
				if gotPub != tc.sourceDisplay {
					t.Errorf("PublicationTitle = %q, want %q", gotPub, tc.sourceDisplay)
				}
				if gotProc != "" || gotBook != "" {
					t.Errorf("other title fields must be empty, got proc=%q book=%q", gotProc, gotBook)
				}
			case "proceedingsTitle":
				if gotProc != tc.sourceDisplay {
					t.Errorf("ProceedingsTitle = %q, want %q", gotProc, tc.sourceDisplay)
				}
				if gotPub != "" {
					t.Errorf("PublicationTitle must be empty for %s, got %q", tc.openalexType, gotPub)
				}
			case "bookTitle":
				if gotBook != tc.sourceDisplay {
					t.Errorf("BookTitle = %q, want %q", gotBook, tc.sourceDisplay)
				}
				if gotPub != "" {
					t.Errorf("PublicationTitle must be empty for %s, got %q", tc.openalexType, gotPub)
				}
			case "":
				if gotPub != "" || gotProc != "" || gotBook != "" {
					t.Errorf("no title field should be set for %s, got pub=%q proc=%q book=%q", tc.openalexType, gotPub, gotProc, gotBook)
				}
			}
		})
	}
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestToItemFields_institutionalCreator(t *testing.T) {
	t.Parallel()
	w := &openalex.Work{
		ID: "https://openalex.org/W1",
		Authorships: []openalex.Authorship{
			{Author: openalex.AuthorRef{DisplayName: "NASA"}},
			{Author: openalex.AuthorRef{DisplayName: "World Health Organization"}}, // multi-word but no comma → still institutional? handled as two-name split.
		},
	}
	got := ToItemFields(w)
	if got.Creators == nil || len(*got.Creators) != 2 {
		t.Fatalf("creators = %v", got.Creators)
	}
	nasa := (*got.Creators)[0]
	if nasa.Name == nil || *nasa.Name != "NASA" || nasa.FirstName != nil || nasa.LastName != nil {
		t.Errorf("single-token name must be institutional, got %+v", nasa)
	}
}

func TestToItemFields_yearOnlyDate(t *testing.T) {
	t.Parallel()
	// OpenAlex sometimes omits publication_date but keeps publication_year.
	w := &openalex.Work{
		ID:              "https://openalex.org/W1",
		PublicationYear: intPtr(1871),
	}
	got := ToItemFields(w)
	if got.Date == nil || *got.Date != "1871" {
		t.Errorf("Date = %v, want \"1871\"", got.Date)
	}
}

func TestToItemFields_titleFallsBackToDisplayName(t *testing.T) {
	t.Parallel()
	dn := "Display Name Fallback"
	got := ToItemFields(&openalex.Work{ID: "https://openalex.org/W1", DisplayName: &dn})
	if got.Title == nil || *got.Title != dn {
		t.Errorf("Title = %v", got.Title)
	}
}

func TestToItemFields_doiStrippingVariants(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"https://doi.org/10.1038/x", "10.1038/x"},
		{"http://doi.org/10.1038/x", "10.1038/x"},
		{"https://dx.doi.org/10.1038/x", "10.1038/x"},
		{"10.1038/x", "10.1038/x"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			doi := tc.in
			got := ToItemFields(&openalex.Work{ID: "https://openalex.org/W1", DOI: &doi})
			if got.DOI == nil || *got.DOI != tc.want {
				t.Errorf("DOI = %v, want %q", got.DOI, tc.want)
			}
		})
	}
}

func TestToItemFields_abstractReconstructsWithRepeatedTokens(t *testing.T) {
	t.Parallel()
	// "the cat saw the dog" — "the" at positions 0 and 3
	w := &openalex.Work{
		ID: "https://openalex.org/W1",
		AbstractInvertedIndex: map[string][]int{
			"the": {0, 3}, "cat": {1}, "saw": {2}, "dog": {4},
		},
	}
	got := ToItemFields(w)
	if got.AbstractNote == nil || *got.AbstractNote != "the cat saw the dog" {
		t.Errorf("AbstractNote = %v", got.AbstractNote)
	}
}

func TestToItemFields_nilWorkIsSafe(t *testing.T) {
	t.Parallel()
	// Minimal work: only the required ID. Must not panic and must still
	// produce a valid ItemData with a sensible default itemType.
	got := ToItemFields(&openalex.Work{ID: "https://openalex.org/W1"})
	if got.ItemType == "" {
		t.Error("ItemType must default, got empty")
	}
	if got.Extra == nil || !strings.Contains(*got.Extra, "W1") {
		t.Errorf("Extra = %v", got.Extra)
	}
}

func TestExtractOpenAlexShortID(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"https://openalex.org/W2086842081", "W2086842081"},
		{"http://openalex.org/A5061940714", "A5061940714"},
		{"W1234567", "W1234567"},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractOpenAlexShortID(tc.in)
		if got != tc.want {
			t.Errorf("extractOpenAlexShortID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
