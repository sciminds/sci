package markdb

import (
	"regexp"
	"strings"
)

// RawLink represents a link extracted from markdown content.
type RawLink struct {
	Raw        string // original syntax as found in text
	TargetPath string // resolved path or filename
	Fragment   string // #heading part
	Alias      string // display text
	Line       int    // 1-based line number
}

var (
	reWikilinkExtract = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	reMdLinkExtract   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reInlineCodeSpan  = regexp.MustCompile("`[^`]+`")
)

// ExtractLinks finds all wikilinks and markdown links to .md files in the body.
// Links inside fenced code blocks and inline code spans are ignored.
func ExtractLinks(body string) []RawLink {
	lines := strings.Split(body, "\n")
	var links []RawLink
	inFence := false

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track fenced code blocks.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		// Remove inline code spans before scanning for links.
		cleaned := reInlineCodeSpan.ReplaceAllString(line, "")

		// Extract wikilinks: [[target]], [[target|alias]], [[target#frag]], [[target#frag|alias]]
		for _, match := range reWikilinkExtract.FindAllStringSubmatch(cleaned, -1) {
			raw := match[0]
			inner := match[1]

			var target, fragment, alias string

			// Split alias first (pipe).
			if idx := strings.Index(inner, "|"); idx >= 0 {
				alias = inner[idx+1:]
				inner = inner[:idx]
			}

			// Split fragment (hash).
			if idx := strings.Index(inner, "#"); idx >= 0 {
				fragment = inner[idx+1:]
				inner = inner[:idx]
			}

			target = inner

			links = append(links, RawLink{
				Raw:        raw,
				TargetPath: target,
				Fragment:   fragment,
				Alias:      alias,
				Line:       lineNum + 1,
			})
		}

		// Extract markdown links: [text](path.md) or [text](path.md#frag)
		for _, match := range reMdLinkExtract.FindAllStringSubmatch(cleaned, -1) {
			raw := match[0]
			alias := match[1]
			href := match[2]

			// Skip external URLs.
			if strings.Contains(href, "://") {
				continue
			}

			// Split fragment from href.
			var fragment string
			if idx := strings.Index(href, "#"); idx >= 0 {
				fragment = href[idx+1:]
				href = href[:idx]
			}

			// Only track links to .md files.
			if !strings.HasSuffix(strings.ToLower(href), ".md") {
				continue
			}

			links = append(links, RawLink{
				Raw:        raw,
				TargetPath: href,
				Fragment:   fragment,
				Alias:      alias,
				Line:       lineNum + 1,
			})
		}
	}

	return links
}
