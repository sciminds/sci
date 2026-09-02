// Package doiorg resolves a DOI against doi.org, the authoritative registry.
//
// It answers the one question a citation index answers badly: does this
// identifier exist at all, and what is it? OpenAlex and friends are
// indexes — comprehensive for journal articles, patchy for monographs,
// chapters and unindexed preprints. doi.org sits in front of every
// registrar (Crossref, DataCite, mEDRA, …), so a 404 there means the DOI
// was never registered by anyone.
//
// The mechanism is content negotiation: asking doi.org for
// application/vnd.citationstyles.csl+json returns CSL-JSON metadata instead
// of the usual 302 to the publisher's landing page. Decoding and the map
// onto a Zotero item live in internal/zot/csl, which is pure; this package
// is the network half, built as the doi.org lookup behind `sci zot item
// add`, and one of the three packages under internal/zot/ allowed to
// reach net/http (lint-guard rule 18: api, doiorg, cli).
package doiorg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sciminds/sci/internal/doi"
	"github.com/sciminds/sci/internal/zot/csl"
)

// ErrNotFound means doi.org has no record of the DOI — it was never
// registered with any registrar. Callers tell it apart from a transport
// failure with errors.Is: the first is a fact about the DOI, the second
// says nothing about it.
var ErrNotFound = errors.New("doiorg: DOI not registered")

// DefaultBaseURL is the public resolver.
const DefaultBaseURL = "https://doi.org"

// cslContentType is the content-negotiation type that makes doi.org return
// metadata rather than redirect to the publisher.
const cslContentType = "application/vnd.citationstyles.csl+json"

// userAgent identifies sci to the resolver, as doi.org's terms ask.
const userAgent = "sci (https://github.com/sciminds/sci)"

// Record is the resolved CSL record; it is [csl.Record], re-exported so a
// caller holding one need not import csl to name it.
type Record = csl.Record

// Client resolves DOIs against a doi.org-compatible resolver. BaseURL and
// HTTP are exported so tests can point it at a fake.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client pointed at the public doi.org resolver with a
// 20-second timeout.
func New() *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Resolve fetches the registry record for a DOI in any cited form (bare,
// doi:-prefixed, or a resolver URL). It returns [ErrNotFound] when the DOI
// is unregistered and an ordinary error when the question could not be
// asked.
func (c *Client) Resolve(ctx context.Context, rawDOI string) (*Record, error) {
	id := Normalize(rawDOI)
	target, err := c.recordURL(id)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("doiorg: %w", err)
	}
	req.Header.Set("Accept", cslContentType)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doiorg: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%s: %w", id, ErrNotFound)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("doiorg %s: HTTP %d: %s", id, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("doiorg %s: read body: %w", id, err)
	}
	rec, err := csl.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("doiorg %s: %w", id, err)
	}
	return rec, nil
}

// recordURL is BaseURL with the DOI as its path, escaped. A DOI may carry
// `#`, `?` and `%` (Wiley SICIs, legacy registrants); pasted raw into a
// URL string, `#` and `?` would end the path early and `%` would corrupt
// it — and a truncated DOI resolves to the WRONG record rather than
// failing. Setting url.URL.Path lets String() escape the whole thing.
func (c *Client) recordURL(id string) (string, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", fmt.Errorf("doiorg: base URL %q: %w", c.BaseURL, err)
	}
	target := *base
	target.Path = strings.TrimSuffix(base.Path, "/") + "/" + id
	target.RawPath, target.RawQuery, target.Fragment = "", "", ""
	return target.String(), nil
}

// Normalize trims the resolver-URL and "doi:" prefixes (and surrounding
// whitespace) so any cited form reaches the registry as a bare DOI.
func Normalize(rawDOI string) string {
	return doi.StripResolver(rawDOI)
}

// arxivVersion is the trailing vN an arXiv id may carry.
var arxivVersion = regexp.MustCompile(`v\d+$`)

// ArxivDOI maps an arXiv id to the DataCite DOI arXiv registers for it, so
// preprints resolve through the same registry as everything else. The
// version suffix is dropped — arXiv registers one DOI per paper, not per
// version.
func ArxivDOI(id string) string {
	return "10.48550/arXiv." + arxivVersion.ReplaceAllString(strings.TrimSpace(id), "")
}
