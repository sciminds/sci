package markdb

import (
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "headings stripped",
			input: "# Title\n## Sub\n### Deep",
			want:  "Title\nSub\nDeep",
		},
		{
			name:  "bold and italic",
			input: "**bold** and *italic* and ***both***",
			want:  "bold and italic and both",
		},
		{
			name:  "inline code",
			input: "use `fmt.Println` here",
			want:  "use fmt.Println here",
		},
		{
			name:  "markdown links",
			input: "see [the docs](https://example.com) for info",
			want:  "see the docs for info",
		},
		{
			name:  "wikilinks",
			input: "link to [[My Note]] and [[Other|display]]",
			want:  "link to My Note and display",
		},
		{
			name:  "fenced code block removed",
			input: "before\n```go\nfmt.Println(\"hi\")\n```\nafter",
			want:  "before\nafter",
		},
		{
			name:  "unordered list markers",
			input: "- first\n- second\n* third",
			want:  "first\nsecond\nthird",
		},
		{
			name:  "ordered list markers",
			input: "1. first\n2. second\n10. tenth",
			want:  "first\nsecond\ntenth",
		},
		{
			name:  "blockquotes",
			input: "> quoted text\n> more quoted",
			want:  "quoted text\nmore quoted",
		},
		{
			name:  "images",
			input: "see ![alt text](image.png) here",
			want:  "see alt text here",
		},
		{
			name:  "html tags",
			input: "<em>emphasized</em> and <strong>bold</strong>",
			want:  "emphasized and bold",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "plain text passthrough",
			input: "just some normal text",
			want:  "just some normal text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("StripMarkdown() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFlattenFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "nil map",
			input: nil,
			want:  "",
		},
		{
			name:  "empty map",
			input: map[string]any{},
			want:  "",
		},
		{
			name:  "scalar values",
			input: map[string]any{"title": "Hello", "count": 42},
			// order not guaranteed, test containment instead
		},
		{
			name:  "list values",
			input: map[string]any{"tags": []any{"go", "sqlite"}},
			// should contain "tags: go, sqlite"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FlattenFrontmatter(tt.input)
			switch tt.name {
			case "nil map", "empty map":
				if got != tt.want {
					t.Errorf("FlattenFrontmatter() = %q, want %q", got, tt.want)
				}
			case "scalar values":
				if !containsLine(got, "title: Hello") {
					t.Errorf("missing 'title: Hello' in %q", got)
				}
				if !containsLine(got, "count: 42") {
					t.Errorf("missing 'count: 42' in %q", got)
				}
			case "list values":
				if !containsLine(got, "tags: go, sqlite") {
					t.Errorf("missing 'tags: go, sqlite' in %q", got)
				}
			}
		})
	}
}

func containsLine(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return append([]string{}, split(s, "\n")...)
}

func split(s, sep string) []string {
	result := []string{}
	for {
		i := indexOf(s, sep)
		if i < 0 {
			result = append(result, s)
			break
		}
		result = append(result, s[:i])
		s = s[i+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
