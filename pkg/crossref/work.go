package crossref

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"
)

// institution is the nested shape posted-content uses to name its
// repository.
type institution struct {
	Name string `json:"name"`
}

// Contributor is one person on a work's byline, as Crossref records them.
// Name is the literal form some records use instead of given/family
// (consortia, single-name authors); exactly one spelling is populated.
type Contributor struct {
	Given    string `json:"given,omitempty"`
	Family   string `json:"family,omitempty"`
	Name     string `json:"name,omitempty"`
	Sequence string `json:"sequence,omitempty"`
}

// WorkRecord is one work fetched by its own DOI — the publisher-registered
// facts a metadata repair can cite as evidence. Where Record carries just
// enough to apply an exact-title rule, this carries the byline, the venue,
// and the locator fields, because a by-DOI lookup is an identity, not a
// candidate.
type WorkRecord struct {
	DOI       string `json:"doi"`
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	Title     string `json:"title"`
	Venue     string `json:"venue,omitempty"`
	Year      int    `json:"year,omitempty"`
	Volume    string `json:"volume,omitempty"`
	Issue     string `json:"issue,omitempty"`
	Pages     string `json:"pages,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	// Institutions and GroupTitle are how posted-content names its home:
	// the repository arrives as institution[].name ("bioRxiv") and the
	// server's subject grouping as group-title. Both empty on articles.
	Institutions []string      `json:"institutions,omitempty"`
	GroupTitle   string        `json:"group_title,omitempty"`
	Authors      []Contributor `json:"authors,omitempty"`
	Editors      []Contributor `json:"editors,omitempty"`
}

// workResponse mirrors the /works/{doi} envelope: one message, not a list.
type workResponse struct {
	Message struct {
		DOI            string        `json:"DOI"`
		Type           string        `json:"type"`
		Subtype        string        `json:"subtype"`
		Title          []string      `json:"title"`
		ContainerTitle []string      `json:"container-title"`
		Volume         string        `json:"volume"`
		Issue          string        `json:"issue"`
		Page           string        `json:"page"`
		Publisher      string        `json:"publisher"`
		GroupTitle     string        `json:"group-title"`
		Institution    []institution `json:"institution"`
		Author         []Contributor `json:"author"`
		Editor         []Contributor `json:"editor"`
		Issued         struct {
			DateParts [][]int `json:"date-parts"`
		} `json:"issued"`
	} `json:"message"`
}

// Work fetches one work by its own DOI.
//
// A 404 returns (nil, nil): Crossref genuinely lacks records for most
// preprint servers' early years, many book chapters, and everything
// registered elsewhere (DataCite), so "no record" is an ordinary answer a
// sweep must count, and letting it surface as an error would let a flaky
// network masquerade as evidence about a paper — the same rule Search
// applies to an empty candidate list. Any other non-2xx status is an error.
func (c *Client) Work(ctx context.Context, doi string) (*WorkRecord, error) {
	// Percent-encode the DOI, slash included: Crossref accepts both
	// spellings, and the encoded one cannot be misread as path segments.
	u := c.BaseURL + "/works/" + url.PathEscape(strings.TrimSpace(doi))
	if c.Mailto != "" {
		u += "?mailto=" + url.QueryEscape(c.Mailto)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent(c.Mailto))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crossref: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("crossref %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw workResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("crossref decode: %w", err)
	}
	m := raw.Message

	rec := &WorkRecord{
		DOI:        strings.ToLower(m.DOI),
		Type:       m.Type,
		Subtype:    m.Subtype,
		Volume:     m.Volume,
		Issue:      m.Issue,
		Pages:      m.Page,
		Publisher:  m.Publisher,
		GroupTitle: m.GroupTitle,
		Authors:    m.Author,
		Editors:    m.Editor,
	}
	if len(m.Title) > 0 {
		rec.Title = m.Title[0]
	}
	if len(m.ContainerTitle) > 0 {
		rec.Venue = m.ContainerTitle[0]
	}
	rec.Institutions = lo.FilterMap(m.Institution, func(inst institution, _ int) (string, bool) {
		return inst.Name, inst.Name != ""
	})
	// date-parts is [[year, month, day]] with month and day optional, and
	// occasionally [[]] for a work with no stated date.
	if dp := m.Issued.DateParts; len(dp) > 0 && len(dp[0]) > 0 {
		rec.Year = dp[0][0]
	}
	return rec, nil
}
