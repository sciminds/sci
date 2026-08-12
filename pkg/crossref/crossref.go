// Package crossref searches Crossref's REST API by bibliographic text.
//
// It exists to answer one question OpenAlex cannot answer about itself:
// given a title, is there a SECOND index that lands on the same DOI?
//
// That matters because a DOI inferred from a title match is not the same
// claim as a DOI minted by a publisher, and writing the first into a
// library makes it indistinguishable from the second forever after. Two
// independent indexes agreeing is evidence; one index answering alone is
// an inference. Crossref is genuinely independent of OpenAlex — different
// corpus, different matching, different failure modes — and, unlike
// OpenAlex, it is free and unmetered, so the check costs nothing to run
// over a whole library.
//
// This package FETCHES AND DOES NOT CHOOSE. Crossref ranks fuzzily, and
// its top hit for "The serial position effect of free recall" is a
// PsycEXTRA dataset with a similar name rather than Murdock 1962. Deciding
// which candidate IS the item requires title_norm, which is defined
// exactly once, in zot. So Search returns every candidate in Crossref's
// order and lets the caller apply that rule.
package crossref

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the public REST API.
const DefaultBaseURL = "https://api.crossref.org"

// DefaultRows is how many candidates a lookup keeps. Crossref's ranking is
// fuzzy enough that the true match is sometimes second or third, and a
// caller filtering on exact normalized title needs the real record to be
// somewhere in the list at all.
const DefaultRows = 8

// Client searches a Crossref-compatible REST API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Mailto is the contact address for Crossref's polite pool. Without it
	// a library-wide sweep gets rate-limited partway through, which does
	// not fail loudly — it just gets slower and then starts erroring.
	Mailto string
}

// New returns a Client pointed at the public API. mailto may be empty, at
// the cost of the polite pool.
func New(mailto string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Mailto:  mailto,
	}
}

// Record is one candidate: enough to apply an exact-title-and-year rule
// and no more.
type Record struct {
	DOI   string `json:"doi"`
	Title string `json:"title"`
	Venue string `json:"venue"`
	Year  int    `json:"year"`
	// Type is Crossref's work type ("journal-article", "posted-content",
	// "book-chapter"). Kept because a preprint and its published version
	// are both correct answers to the same title, and only the caller
	// knows which one the library holds.
	Type string `json:"type"`
}

// restResponse mirrors the REST envelope. Note this is NOT the CSL JSON
// shape doi.org returns under content negotiation: the fields are named
// alike but arrive wrapped in message.items, and every title is an array.
type restResponse struct {
	Message struct {
		TotalResults int `json:"total-results"`
		Items        []struct {
			DOI            string   `json:"DOI"`
			Type           string   `json:"type"`
			Title          []string `json:"title"`
			ContainerTitle []string `json:"container-title"`
			Issued         struct {
				DateParts [][]int `json:"date-parts"`
			} `json:"issued"`
		} `json:"items"`
	} `json:"message"`
}

// Search returns Crossref's candidates for a bibliographic query, in
// Crossref's own order, with no filtering applied.
//
// An empty result is not an error. Crossref has no record for preprints,
// most pre-1950 papers, and many book chapters — roughly a third of this
// library — and turning the ordinary answer into a failure would both
// drown a sweep in errors and let a transport fault masquerade as "no such
// paper".
func (c *Client) Search(ctx context.Context, title string, rows int) ([]Record, error) {
	if rows <= 0 {
		rows = DefaultRows
	}
	q := url.Values{}
	// Crossref's query params carry no filter DSL — commas and colons are
	// literal here, unlike OpenAlex's filter, where a comma splits the
	// value and takes the whole request with it. The title goes verbatim;
	// escaping it would only lose matches.
	q.Set("query.bibliographic", title)
	q.Set("rows", strconv.Itoa(rows))
	q.Set("select", "DOI,title,container-title,issued,type")
	if c.Mailto != "" {
		q.Set("mailto", c.Mailto)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/works?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent(c.Mailto))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crossref: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("crossref %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw restResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("crossref decode: %w", err)
	}

	out := make([]Record, 0, len(raw.Message.Items))
	for _, it := range raw.Message.Items {
		r := Record{DOI: strings.ToLower(it.DOI), Type: it.Type}
		if len(it.Title) > 0 {
			r.Title = it.Title[0]
		}
		if len(it.ContainerTitle) > 0 {
			r.Venue = it.ContainerTitle[0]
		}
		// date-parts is [[year, month, day]] with month and day optional,
		// and occasionally [[]] for a work with no stated date.
		if len(it.Issued.DateParts) > 0 && len(it.Issued.DateParts[0]) > 0 {
			r.Year = it.Issued.DateParts[0][0]
		}
		out = append(out, r)
	}
	return out, nil
}

func userAgent(mailto string) string {
	ua := "sci-zot/1.0 (https://github.com/sciminds/sci"
	if mailto != "" {
		ua += "; mailto:" + mailto
	}
	return ua + ")"
}
