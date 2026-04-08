package markdb

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParsedFile holds the result of extracting frontmatter from a markdown file.
type ParsedFile struct {
	FrontmatterRaw string
	Frontmatter    map[string]any
	Body           string
	ParseError     string
}

const delimiter = "---"

// ExtractFrontmatter splits a file into YAML frontmatter and body content.
// The raw YAML is preserved verbatim for lossless round-trip export.
func ExtractFrontmatter(content []byte) ParsedFile {
	s := string(content)
	lines := strings.SplitAfter(s, "\n")

	// First line must be exactly "---" (with optional trailing newline).
	if strings.TrimRight(lines[0], "\n") != delimiter {
		return ParsedFile{Body: s}
	}

	// Find the closing delimiter.
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\n") == delimiter {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		// No closing delimiter — treat entire content as body.
		return ParsedFile{Body: s}
	}

	raw := strings.Join(lines[1:closeIdx], "")
	body := strings.Join(lines[closeIdx+1:], "")

	result := ParsedFile{
		FrontmatterRaw: raw,
		Body:           body,
	}

	// Parse the YAML.
	if strings.TrimSpace(raw) == "" {
		result.Frontmatter = map[string]any{}
		result.FrontmatterRaw = ""
		return result
	}

	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		result.ParseError = err.Error()
		return result
	}
	result.Frontmatter = m
	return result
}
