// Package api provides shared utilities for Canvas and GitHub API clients.
package api

import "regexp"

// linkNextRe extracts the URL from a Link header with rel="next".
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// ParseNextLink extracts the next-page URL from a Link header value.
// Returns "" if no next link is found.
func ParseNextLink(header string) string {
	m := linkNextRe.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	return m[1]
}
