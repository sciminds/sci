// Package doiorg resolves a DOI against doi.org, the authoritative registry.
//
// It exists to answer one question that citation indexes answer badly: does
// this identifier exist at all? OpenAlex, Semantic Scholar and friends are
// indexes — comprehensive for journal articles, patchy for monographs, book
// chapters and unindexed preprints. doi.org sits in front of every registrar
// (Crossref, DataCite, mEDRA…), so a 404 there means the DOI was never
// registered by anyone. That is the difference between "our index doesn't
// have it" and "this citation was invented", and `zot bib --verify` reports
// those very differently.
//
// The mechanism is content negotiation: asking doi.org for
// application/vnd.citationstyles.csl+json returns CSL JSON metadata instead
// of the usual 302 to the publisher's landing page.
package doiorg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotFound means doi.org has no record of the DOI — it was never
// registered with any registrar.
var ErrNotFound = errors.New("doiorg: DOI not registered")

// DefaultBaseURL is the public resolver.
const DefaultBaseURL = "https://doi.org"

// cslContentType is the content-negotiation type that makes doi.org return
// metadata rather than redirect to the publisher.
const cslContentType = "application/vnd.citationstyles.csl+json"

// Client resolves DOIs against a doi.org-compatible resolver.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client pointed at the public doi.org resolver.
func New() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Record is the slice of CSL JSON worth keeping: enough to recognize the
// work and confirm it is real.
type Record struct {
	DOI   string
	Title string
	Year  int
	Venue string
	// Type is the CSL type ("journal-article", "monograph", "posted-content").
	Type string
}

// cslRecord mirrors the CSL JSON fields we read. Title is deliberately
// json.RawMessage: registrants serialize it as either a string or a list of
// strings, and a strict string field silently drops the array form.
type cslRecord struct {
	DOI            string          `json:"DOI"`
	Type           string          `json:"type"`
	Title          json.RawMessage `json:"title"`
	ContainerTitle json.RawMessage `json:"container-title"`
	Publisher      string          `json:"publisher"`
	Issued         struct {
		DateParts [][]int `json:"date-parts"`
	} `json:"issued"`
}

// Resolve fetches the registry record for doi. It returns [ErrNotFound] when
// the DOI is unregistered and an ordinary error when the question could not
// be asked — callers must not conflate the two.
func (c *Client) Resolve(ctx context.Context, doi string) (*Record, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+Normalize(doi), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", cslContentType)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doiorg: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("doiorg %s: %d — %s", doi, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw cslRecord
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("doiorg decode: %w", err)
	}

	rec := &Record{
		DOI:   raw.DOI,
		Type:  raw.Type,
		Title: firstString(raw.Title),
		Venue: firstString(raw.ContainerTitle),
	}
	if rec.Venue == "" {
		rec.Venue = raw.Publisher
	}
	// date-parts is [[year, month, day]] with month and day optional.
	if len(raw.Issued.DateParts) > 0 && len(raw.Issued.DateParts[0]) > 0 {
		rec.Year = raw.Issued.DateParts[0][0]
	}
	return rec, nil
}

// firstString reads a CSL field that may be either a string or a list of
// strings, returning the single or first value.
func firstString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

// Normalize trims the resolver-URL and "doi:" prefixes so any cited form
// reaches the registry as a bare DOI.
func Normalize(doi string) string {
	d := strings.TrimSpace(doi)
	for _, p := range []string{
		"https://doi.org/", "http://doi.org/",
		"https://dx.doi.org/", "http://dx.doi.org/",
	} {
		if len(d) >= len(p) && strings.EqualFold(d[:len(p)], p) {
			return d[len(p):]
		}
	}
	if len(d) >= 4 && strings.EqualFold(d[:4], "doi:") {
		return d[4:]
	}
	return d
}

// ArxivDOI maps an arXiv id to the DataCite DOI arXiv registers for it, so
// preprints can be checked through the same registry as everything else. The
// version suffix is dropped — arXiv registers one DOI per paper, not per
// version.
func ArxivDOI(id string) string {
	if i := strings.LastIndexByte(id, 'v'); i > 0 {
		if _, err := fmt.Sscanf(id[i+1:], "%d", new(int)); err == nil {
			id = id[:i]
		}
	}
	return "10.48550/arXiv." + id
}
