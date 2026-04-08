package markdb

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	reFencedBlock   = regexp.MustCompile("(?m)^```[^\n]*\n(?s:.*?)^```\\s*$")
	reHeading       = regexp.MustCompile(`(?m)^#{1,6}\s+(.*)$`)
	reImage         = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reMdLink        = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reWikilinkAlias = regexp.MustCompile(`\[\[[^\]|]+\|([^\]]+)\]\]`)
	reWikilink      = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	reBoldItalic3   = regexp.MustCompile(`\*{3}(.+?)\*{3}`)
	reBold          = regexp.MustCompile(`\*{2}(.+?)\*{2}`)
	reItalic        = regexp.MustCompile(`\*(.+?)\*`)
	reInlineCode    = regexp.MustCompile("`([^`]+)`")
	reBlockquote    = regexp.MustCompile(`(?m)^>\s?(.*)$`)
	reUnorderedList = regexp.MustCompile(`(?m)^[\-\*]\s+(.*)$`)
	reOrderedList   = regexp.MustCompile(`(?m)^\d+\.\s+(.*)$`)
	reHTMLTag       = regexp.MustCompile(`<[^>]+>`)
)

// StripMarkdown converts markdown to plaintext by removing formatting syntax.
func StripMarkdown(md string) string {
	if md == "" {
		return ""
	}
	s := md

	// Remove fenced code blocks first (before inline processing).
	s = reFencedBlock.ReplaceAllString(s, "")

	// Headings.
	s = reHeading.ReplaceAllString(s, "$1")

	// Images before links (images are a superset of link syntax).
	s = reImage.ReplaceAllString(s, "$1")

	// Wikilinks with alias before plain wikilinks.
	s = reWikilinkAlias.ReplaceAllString(s, "$1")
	s = reWikilink.ReplaceAllString(s, "$1")

	// Markdown links.
	s = reMdLink.ReplaceAllString(s, "$1")

	// Bold/italic (triple before double before single).
	s = reBoldItalic3.ReplaceAllString(s, "$1")
	s = reBold.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1")

	// Inline code.
	s = reInlineCode.ReplaceAllString(s, "$1")

	// Blockquotes.
	s = reBlockquote.ReplaceAllString(s, "$1")

	// List markers.
	s = reUnorderedList.ReplaceAllString(s, "$1")
	s = reOrderedList.ReplaceAllString(s, "$1")

	// HTML tags.
	s = reHTMLTag.ReplaceAllString(s, "")

	// Clean up: collapse blank lines from removed blocks.
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}

// FlattenFrontmatter converts a frontmatter map to a flat text representation
// for full-text search indexing.
func FlattenFrontmatter(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		switch val := v.(type) {
		case []any:
			parts := make([]string, len(val))
			for i, item := range val {
				parts[i] = fmt.Sprintf("%v", item)
			}
			lines = append(lines, fmt.Sprintf("%s: %s", k, strings.Join(parts, ", ")))
		default:
			lines = append(lines, fmt.Sprintf("%s: %v", k, v))
		}
	}
	return strings.Join(lines, "\n")
}
