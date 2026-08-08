package openalex

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// OpenAlex entity-ID shapes: W=Works, A=Authors, I=Institutions, S=Sources,
// C=Concepts, F=Funders, P=Publishers, T=Topics.
var (
	openalexID = regexp.MustCompile(`(?i)^[waiscfpt]\d{4,}$`)
	doiRe      = regexp.MustCompile(`^10\.\d{4,9}/\S+$`)
	arxivRe    = regexp.MustCompile(`(?i)^(arxiv:)?\d{4}\.\d{4,5}(v\d+)?$`)
	pmidRe     = regexp.MustCompile(`^\d{6,9}$`)
)

// NormalizeID turns a user-supplied identifier into the form OpenAlex expects
// as the {id} segment of /works/{id}. Accepted inputs include bare OpenAlex
// short IDs (W4389428231), bare DOIs (10.xxx/yyy), doi.org URLs, arXiv IDs,
// PMIDs, and any form already carrying an explicit prefix (doi:, pmid:, etc.).
func NormalizeID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("openalex: empty identifier")
	}

	// Already prefixed (doi:, pmid:, mag:, openalex:, arxiv:) → lowercase the
	// prefix (namespace is case-insensitive on OpenAlex's side), keep the body.
	if i := strings.IndexByte(s, ':'); i > 0 && !strings.EqualFold(s[:min(4, len(s))], "http") {
		return strings.ToLower(s[:i]) + s[i:], nil
	}

	if bare := stripDOIURL(s); bare != "" {
		return "doi:" + bare, nil
	}
	if openalexID.MatchString(s) {
		return strings.ToUpper(s), nil
	}
	if doiRe.MatchString(s) {
		return "doi:" + s, nil
	}
	if arxivRe.MatchString(s) {
		return "arxiv:" + strings.TrimPrefix(strings.ToLower(s), "arxiv:"), nil
	}
	if pmidRe.MatchString(s) {
		return "pmid:" + s, nil
	}
	return "", fmt.Errorf("openalex: unrecognized identifier %q", s)
}

func stripDOIURL(s string) string {
	lower := strings.ToLower(s)
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(lower, p) {
			return s[len(p):]
		}
	}
	return ""
}

// ResolveWork fetches a Work by any accepted identifier form.
func (c *Client) ResolveWork(ctx context.Context, identifier string) (*Work, error) {
	id, err := NormalizeID(identifier)
	if err != nil {
		return nil, err
	}
	var w Work
	if err := c.Get(ctx, "/works/"+id, nil, &w); err != nil {
		return nil, err
	}
	return &w, nil
}
