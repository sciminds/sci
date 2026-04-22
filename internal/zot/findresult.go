package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// FindWorksResult wraps a page of Works returned by `zot find works`.
//
// JSON() emits a compact per-work shape by default (~10 fields) — chosen
// to match the cognitive budget of an LLM agent: title, authors, DOI,
// year, OA status, and a few enough ranking signals. Set Verbose=true
// (from the CLI via --verbose) to pass through the full openalex.Work
// with every nested object, ~100+ lines per work.
type FindWorksResult struct {
	Query      string          `json:"query"`
	Total      int             `json:"total"` // server-reported match count
	Count      int             `json:"count"` // len(Works) in this page
	NextCursor string          `json:"next_cursor,omitempty"`
	Works      []openalex.Work `json:"works"`
	Verbose    bool            `json:"-"`
}

// findWorksCompact is the skim-friendly shape emitted by JSON() when Verbose
// is false. Everything needed to decide "do I want this paper?" without
// the raw OpenAlex institution graph.
type findWorksCompactResult struct {
	Query      string            `json:"query"`
	Total      int               `json:"total"`
	Count      int               `json:"count"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Works      []FindWorkCompact `json:"works"`
}

// FindWorkCompact is one work in the compact JSON shape. Zero-value
// optional fields are elided via omitempty.
type FindWorkCompact struct {
	OpenAlexID   string   `json:"openalex_id"`
	DOI          string   `json:"doi,omitempty"`
	Title        string   `json:"title,omitempty"`
	Year         int      `json:"year,omitempty"`
	Type         string   `json:"type,omitempty"`
	Authors      []string `json:"authors,omitempty"`
	Venue        string   `json:"venue,omitempty"`
	CitedByCount int      `json:"cited_by_count"`
	IsOA         bool     `json:"is_oa,omitempty"`
	OAStatus     string   `json:"oa_status,omitempty"`
	PDFURL       string   `json:"pdf_url,omitempty"`
}

func (r FindWorksResult) JSON() any {
	if r.Verbose {
		return r
	}
	return findWorksCompactResult{
		Query:      r.Query,
		Total:      r.Total,
		Count:      r.Count,
		NextCursor: r.NextCursor,
		Works:      lo.Map(r.Works, func(w openalex.Work, _ int) FindWorkCompact { return workToCompact(w) }),
	}
}

func (r FindWorksResult) Human() string {
	return renderFindPage(r.Query, r.Count, r.Total, r.NextCursor, r.Works, writeWorkLine)
}

func workToCompact(w openalex.Work) FindWorkCompact {
	out := FindWorkCompact{
		OpenAlexID:   openalexShortID(w.ID),
		CitedByCount: w.CitedByCount,
		IsOA:         w.IsOA,
	}
	if w.Title != nil && *w.Title != "" {
		out.Title = *w.Title
	} else if w.DisplayName != nil {
		out.Title = *w.DisplayName
	}
	if w.DOI != nil {
		out.DOI = stripDOIPrefix(*w.DOI)
	}
	if w.PublicationYear != nil {
		out.Year = *w.PublicationYear
	}
	if w.Type != nil {
		out.Type = *w.Type
	}
	out.Authors = lo.FilterMap(w.Authorships, func(a openalex.Authorship, _ int) (string, bool) {
		return a.Author.DisplayName, a.Author.DisplayName != ""
	})
	if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil {
		out.Venue = w.PrimaryLocation.Source.DisplayName
	}
	if w.OpenAccess != nil {
		out.OAStatus = w.OpenAccess.OAStatus
	}
	if w.BestOALocation != nil && w.BestOALocation.PDFURL != nil {
		out.PDFURL = *w.BestOALocation.PDFURL
	}
	return out
}

// FindAuthorsResult wraps a page of Authors returned by `zot find authors`.
// Verbose flips JSON() between compact and raw openalex.Author — see
// FindWorksResult for the rationale.
type FindAuthorsResult struct {
	Query      string            `json:"query"`
	Total      int               `json:"total"`
	Count      int               `json:"count"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Authors    []openalex.Author `json:"authors"`
	Verbose    bool              `json:"-"`
}

type findAuthorsCompactResult struct {
	Query      string              `json:"query"`
	Total      int                 `json:"total"`
	Count      int                 `json:"count"`
	NextCursor string              `json:"next_cursor,omitempty"`
	Authors    []FindAuthorCompact `json:"authors"`
}

// FindAuthorCompact is one author in the compact JSON shape.
type FindAuthorCompact struct {
	OpenAlexID   string `json:"openalex_id"`
	DisplayName  string `json:"display_name"`
	ORCID        string `json:"orcid,omitempty"`
	WorksCount   int    `json:"works_count"`
	CitedByCount int    `json:"cited_by_count"`
	HIndex       int    `json:"h_index,omitempty"`
	Institution  string `json:"institution,omitempty"`
}

func (r FindAuthorsResult) JSON() any {
	if r.Verbose {
		return r
	}
	return findAuthorsCompactResult{
		Query:      r.Query,
		Total:      r.Total,
		Count:      r.Count,
		NextCursor: r.NextCursor,
		Authors:    lo.Map(r.Authors, func(a openalex.Author, _ int) FindAuthorCompact { return authorToCompact(a) }),
	}
}

func (r FindAuthorsResult) Human() string {
	return renderFindPage(r.Query, r.Count, r.Total, r.NextCursor, r.Authors, writeAuthorLine)
}

func authorToCompact(a openalex.Author) FindAuthorCompact {
	out := FindAuthorCompact{
		OpenAlexID:   openalexShortID(a.ID),
		DisplayName:  a.DisplayName,
		WorksCount:   a.WorksCount,
		CitedByCount: a.CitedByCount,
	}
	if a.ORCID != nil {
		out.ORCID = stripORCIDPrefix(*a.ORCID)
	}
	if a.SummaryStats != nil {
		out.HIndex = a.SummaryStats.HIndex
	}
	if len(a.LastKnownInstitutions) > 0 {
		out.Institution = a.LastKnownInstitutions[0].DisplayName
	}
	return out
}

// renderFindPage handles the empty-state + per-item + footer scaffolding that's
// identical between Works and Authors pages. writeLine is the per-item writer.
func renderFindPage[T any](query string, count, total int, nextCursor string, items []T, writeLine func(*strings.Builder, T)) string {
	if count == 0 {
		if query != "" {
			return fmt.Sprintf("  %s no results for %q\n", uikit.TUI.Dim().Render("·"), query)
		}
		return fmt.Sprintf("  %s no results\n", uikit.TUI.Dim().Render("·"))
	}
	var b strings.Builder
	for _, it := range items {
		writeLine(&b, it)
	}
	fmt.Fprintf(&b, "\n  %s %d of %d match(es)\n", uikit.SymArrow, count, total)
	if nextCursor != "" {
		fmt.Fprintf(&b, "  %s more: --cursor %s\n", uikit.TUI.Dim().Render("·"), nextCursor)
	}
	return b.String()
}

func writeWorkLine(b *strings.Builder, w openalex.Work) {
	title := "(untitled)"
	if w.Title != nil && *w.Title != "" {
		title = *w.Title
	} else if w.DisplayName != nil {
		title = *w.DisplayName
	}
	year := ""
	if w.PublicationYear != nil {
		year = " " + uikit.TUI.Dim().Render(fmt.Sprintf("(%d)", *w.PublicationYear))
	}
	fmt.Fprintf(b, "  %s  %s%s\n",
		uikit.TUI.TextBlue().Render(openalexShortID(w.ID)),
		title,
		year,
	)
	var meta []string
	if authors := formatAuthorList(w.Authorships); authors != "" {
		meta = append(meta, authors)
	}
	if w.PrimaryLocation != nil && w.PrimaryLocation.Source != nil && w.PrimaryLocation.Source.DisplayName != "" {
		meta = append(meta, w.PrimaryLocation.Source.DisplayName)
	}
	if w.DOI != nil && *w.DOI != "" {
		meta = append(meta, stripDOIPrefix(*w.DOI))
	}
	if len(meta) > 0 {
		fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(strings.Join(meta, " · ")))
	}
}

func writeAuthorLine(b *strings.Builder, a openalex.Author) {
	fmt.Fprintf(b, "  %s  %s\n",
		uikit.TUI.TextBlue().Render(openalexShortID(a.ID)),
		a.DisplayName,
	)
	var meta []string
	meta = append(meta, fmt.Sprintf("%d works", a.WorksCount), fmt.Sprintf("%d citations", a.CitedByCount))
	if a.ORCID != nil && *a.ORCID != "" {
		meta = append(meta, "ORCID: "+stripORCIDPrefix(*a.ORCID))
	}
	fmt.Fprintf(b, "    %s\n", uikit.TUI.Dim().Render(strings.Join(meta, " · ")))
}

// formatAuthorList collapses a Work's authorships into "First et al." /
// "First & Second" for a compact display line.
func formatAuthorList(authorships []openalex.Authorship) string {
	names := lo.FilterMap(authorships, func(a openalex.Authorship, _ int) (string, bool) {
		return surname(a.Author.DisplayName), a.Author.DisplayName != ""
	})
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " & " + names[1]
	default:
		return names[0] + " et al."
	}
}

func surname(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, " \t"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func openalexShortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func stripDOIPrefix(doi string) string {
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(strings.ToLower(doi), p) {
			return doi[len(p):]
		}
	}
	return doi
}

func stripORCIDPrefix(orcid string) string {
	for _, p := range []string{"https://orcid.org/", "http://orcid.org/"} {
		if strings.HasPrefix(strings.ToLower(orcid), p) {
			return orcid[len(p):]
		}
	}
	return orcid
}
