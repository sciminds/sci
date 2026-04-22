// Package enrich converts OpenAlex Work metadata into Zotero ItemData.
//
// Lives in a sub-package for the same reason as internal/zot/fix/: it imports
// both openalex (read) and client (write-side ItemData shapes), so parking it
// here avoids a cycle with the parent zot package's Config. The openalex
// client itself has zero awareness of Zotero types — all coupling lives in
// this package.
package enrich

import (
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/client"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// ToItemFields produces a Zotero ItemData snapshot from an OpenAlex Work.
// Intended for full-item creation (zot add --openalex). It always sets
// ItemType, Title, and Extra (carrying the OpenAlex ID); other fields are
// populated only when the Work has them.
func ToItemFields(w *openalex.Work) client.ItemData {
	out := client.ItemData{ItemType: zoteroItemType(w.Type)}

	if t := pickTitle(w); t != "" {
		out.Title = &t
	}
	if doi := normalizeDOI(w.DOI); doi != "" {
		out.DOI = &doi
	}
	if d := pickDate(w); d != "" {
		out.Date = &d
	}
	if lang := strOr(w.Language, ""); lang != "" {
		out.Language = &lang
	}
	if u := primaryURL(w); u != "" {
		out.Url = &u
	}
	if ptitle := primarySourceName(w); ptitle != "" {
		routeSourceTitle(&out, ptitle)
	}
	if abs := reconstructAbstract(w.AbstractInvertedIndex); abs != "" {
		out.AbstractNote = &abs
	}
	if creators := toCreators(w.Authorships); len(creators) > 0 {
		out.Creators = &creators
	}
	if extra := buildExtra(w); extra != "" {
		out.Extra = &extra
	}
	return out
}

// zoteroItemType maps OpenAlex's `type` vocabulary to Zotero's itemType enum.
// Unmapped / missing inputs fall back to journalArticle (statistically the
// most likely kind for scholarly metadata lookups).
func zoteroItemType(oaType *string) client.ItemDataItemType {
	if oaType == nil {
		return client.JournalArticle
	}
	switch *oaType {
	case "journal-article", "article", "letter", "editorial", "review":
		return client.JournalArticle
	case "book", "monograph":
		return client.Book
	case "book-chapter":
		return client.BookSection
	case "dissertation":
		return client.Thesis
	case "preprint", "posted-content":
		return client.Preprint
	case "proceedings-article":
		return client.ConferencePaper
	case "report", "standard":
		return client.Report
	case "peer-review", "other", "erratum", "paratext", "reference-entry":
		return client.Document
	default:
		return client.JournalArticle
	}
}

func pickTitle(w *openalex.Work) string {
	if w.Title != nil && *w.Title != "" {
		return *w.Title
	}
	if w.DisplayName != nil {
		return *w.DisplayName
	}
	return ""
}

// normalizeDOI strips any doi.org URL wrapper so the stored value is the bare
// 10.xxx/yyy form Zotero expects.
func normalizeDOI(doi *string) string {
	if doi == nil {
		return ""
	}
	s := strings.TrimSpace(*doi)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(strings.ToLower(s), prefix) {
			return s[len(prefix):]
		}
	}
	return s
}

func pickDate(w *openalex.Work) string {
	if w.PublicationDate != nil && *w.PublicationDate != "" {
		return *w.PublicationDate
	}
	if w.PublicationYear != nil {
		return fmt.Sprintf("%d", *w.PublicationYear)
	}
	return ""
}

func primaryURL(w *openalex.Work) string {
	if w.PrimaryLocation != nil && w.PrimaryLocation.LandingPageURL != nil {
		return *w.PrimaryLocation.LandingPageURL
	}
	if w.BestOALocation != nil && w.BestOALocation.LandingPageURL != nil {
		return *w.BestOALocation.LandingPageURL
	}
	return ""
}

func primarySourceName(w *openalex.Work) string {
	if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
		return w.PrimaryLocation.Source.DisplayName
	}
	return ""
}

// routeSourceTitle stores the primary-location source name in the ItemType-
// appropriate Zotero field. Writing publicationTitle on anything other than a
// journalArticle makes Zotero reject the batch with "'publicationTitle' is not
// a valid field for type X"; for preprints / theses / reports we leave the
// source unattached rather than invent a home for it.
func routeSourceTitle(out *client.ItemData, title string) {
	t := title
	switch out.ItemType {
	case client.JournalArticle:
		out.PublicationTitle = &t
	case client.ConferencePaper:
		out.ProceedingsTitle = &t
	case client.BookSection:
		out.BookTitle = &t
	}
}

// toCreators unflattens OpenAlex's flat display_name strings into Zotero's
// firstName/lastName split. Single-token names (institutional authors like
// "NASA") flow through as Name, matching Zotero's one-field convention.
func toCreators(authorships []openalex.Authorship) []client.Creator {
	return lo.Map(authorships, func(a openalex.Authorship, _ int) client.Creator {
		return makeCreator(a.Author.DisplayName)
	})
}

func makeCreator(displayName string) client.Creator {
	name := strings.TrimSpace(displayName)
	// Single-token → institutional (Zotero fieldMode=1).
	if !strings.ContainsAny(name, " \t") {
		n := name
		return client.Creator{CreatorType: "author", Name: &n}
	}
	// Split on the final whitespace — everything before is first/middle, last token is surname.
	i := strings.LastIndexAny(name, " \t")
	first := strings.TrimSpace(name[:i])
	last := strings.TrimSpace(name[i+1:])
	return client.Creator{CreatorType: "author", FirstName: &first, LastName: &last}
}

// reconstructAbstract rebuilds readable text from OpenAlex's inverted-index
// encoding. Each word carries its positions in the original abstract; we
// scatter tokens into a flat slice sized to the max position and join.
//
// Returns "" for empty/nil input so callers can leave AbstractNote unset.
func reconstructAbstract(inv map[string][]int) string {
	if len(inv) == 0 {
		return ""
	}
	maxPos := -1
	for _, positions := range inv {
		for _, p := range positions {
			if p > maxPos {
				maxPos = p
			}
		}
	}
	if maxPos < 0 {
		return ""
	}
	tokens := make([]string, maxPos+1)
	for word, positions := range inv {
		for _, p := range positions {
			if p >= 0 && p <= maxPos {
				tokens[p] = word
			}
		}
	}
	return strings.Join(lo.Filter(tokens, func(s string, _ int) bool { return s != "" }), " ")
}

// buildExtra emits a Zotero "Extra" block carrying identifiers Zotero has no
// first-class field for. Today that's just the OpenAlex ID; later entries
// (ORCID, PMID) follow the same "key: value" per-line convention used by
// citation managers and BBT.
func buildExtra(w *openalex.Work) string {
	lines := make([]string, 0, 1)
	if id := extractOpenAlexShortID(w.ID); id != "" {
		lines = append(lines, "OpenAlex: "+id)
	}
	slices.Sort(lines) // stable ordering across runs
	return strings.Join(lines, "\n")
}

// extractOpenAlexShortID pulls the short ID (W…, A…) out of OpenAlex's full
// canonical URL form. Accepts bare short IDs too.
func extractOpenAlexShortID(id string) string {
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func strOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}
