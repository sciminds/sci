package zot

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot/openalex"
)

// FindWorksResult wraps a page of Works returned by `zot find works`.
type FindWorksResult struct {
	Query      string          `json:"query"`
	Total      int             `json:"total"` // server-reported match count
	Count      int             `json:"count"` // len(Works) in this page
	NextCursor string          `json:"next_cursor,omitempty"`
	Works      []openalex.Work `json:"works"`
}

func (r FindWorksResult) JSON() any { return r }

func (r FindWorksResult) Human() string {
	return renderFindPage(r.Query, r.Count, r.Total, r.NextCursor, r.Works, writeWorkLine)
}

// FindAuthorsResult wraps a page of Authors returned by `zot find authors`.
type FindAuthorsResult struct {
	Query      string            `json:"query"`
	Total      int               `json:"total"`
	Count      int               `json:"count"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Authors    []openalex.Author `json:"authors"`
}

func (r FindAuthorsResult) JSON() any { return r }

func (r FindAuthorsResult) Human() string {
	return renderFindPage(r.Query, r.Count, r.Total, r.NextCursor, r.Authors, writeAuthorLine)
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
