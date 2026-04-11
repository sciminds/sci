package markdb

import (
	"fmt"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []RawLink
	}{
		{
			name:  "simple wikilink",
			input: "see [[note]] here",
			want: []RawLink{
				{Raw: "[[note]]", TargetPath: "note", Line: 1},
			},
		},
		{
			name:  "wikilink with alias",
			input: "see [[note|display text]] here",
			want: []RawLink{
				{Raw: "[[note|display text]]", TargetPath: "note", Alias: "display text", Line: 1},
			},
		},
		{
			name:  "wikilink with fragment",
			input: "see [[note#heading]] here",
			want: []RawLink{
				{Raw: "[[note#heading]]", TargetPath: "note", Fragment: "heading", Line: 1},
			},
		},
		{
			name:  "wikilink with fragment and alias",
			input: "see [[note#heading|display]] here",
			want: []RawLink{
				{Raw: "[[note#heading|display]]", TargetPath: "note", Fragment: "heading", Alias: "display", Line: 1},
			},
		},
		{
			name:  "markdown link to md file",
			input: "see [the docs](other/file.md) here",
			want: []RawLink{
				{Raw: "[the docs](other/file.md)", TargetPath: "other/file.md", Alias: "the docs", Line: 1},
			},
		},
		{
			name:  "markdown link to non-md ignored",
			input: "see [pic](image.png) here",
			want:  nil,
		},
		{
			name:  "external URL ignored",
			input: "see [site](https://example.com) here",
			want:  nil,
		},
		{
			name:  "markdown link with fragment",
			input: "see [docs](file.md#section) here",
			want: []RawLink{
				{Raw: "[docs](file.md#section)", TargetPath: "file.md", Fragment: "section", Alias: "docs", Line: 1},
			},
		},
		{
			name:  "two links on one line",
			input: "link [[one]] and [[two]]",
			want: []RawLink{
				{Raw: "[[one]]", TargetPath: "one", Line: 1},
				{Raw: "[[two]]", TargetPath: "two", Line: 1},
			},
		},
		{
			name:  "links across lines",
			input: "first [[one]]\nsecond [[two]]\nthird [[three]]",
			want: []RawLink{
				{Raw: "[[one]]", TargetPath: "one", Line: 1},
				{Raw: "[[two]]", TargetPath: "two", Line: 2},
				{Raw: "[[three]]", TargetPath: "three", Line: 3},
			},
		},
		{
			name:  "links inside fenced code block ignored",
			input: "before [[real]]\n```\n[[fake]]\n```\nafter [[also real]]",
			want: []RawLink{
				{Raw: "[[real]]", TargetPath: "real", Line: 1},
				{Raw: "[[also real]]", TargetPath: "also real", Line: 5},
			},
		},
		{
			name:  "links inside inline code ignored",
			input: "see `[[not a link]]` and [[real link]]",
			want: []RawLink{
				{Raw: "[[real link]]", TargetPath: "real link", Line: 1},
			},
		},
		{
			name:  "no links",
			input: "just plain text",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinks(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d links, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i].Raw != tt.want[i].Raw {
					t.Errorf("[%d] Raw = %q, want %q", i, got[i].Raw, tt.want[i].Raw)
				}
				if got[i].TargetPath != tt.want[i].TargetPath {
					t.Errorf("[%d] TargetPath = %q, want %q", i, got[i].TargetPath, tt.want[i].TargetPath)
				}
				if got[i].Fragment != tt.want[i].Fragment {
					t.Errorf("[%d] Fragment = %q, want %q", i, got[i].Fragment, tt.want[i].Fragment)
				}
				if got[i].Alias != tt.want[i].Alias {
					t.Errorf("[%d] Alias = %q, want %q", i, got[i].Alias, tt.want[i].Alias)
				}
				if got[i].Line != tt.want[i].Line {
					t.Errorf("[%d] Line = %d, want %d", i, got[i].Line, tt.want[i].Line)
				}
			}
		})
	}
}

func ExampleExtractLinks() {
	links := ExtractLinks("see [[note]] and [guide](guide.md)")
	for _, l := range links {
		fmt.Printf("%s -> %s\n", l.Raw, l.TargetPath)
	}
	// Output:
	// [[note]] -> note
	// [guide](guide.md) -> guide.md
}
