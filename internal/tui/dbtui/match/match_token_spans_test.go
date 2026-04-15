package match

import (
	"slices"
	"testing"
)

func TestTokenSpansInText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		tokens []string
		text   string
		want   []int
	}{
		{
			name:   "empty tokens",
			tokens: nil,
			text:   "hello",
			want:   nil,
		},
		{
			name:   "empty text",
			tokens: []string{"foo"},
			text:   "",
			want:   nil,
		},
		{
			name:   "single token, one hit",
			tokens: []string{"bar"},
			text:   "foobarbaz",
			want:   []int{3, 4, 5},
		},
		{
			name:   "case insensitive",
			tokens: []string{"BAR"},
			text:   "foobarbaz",
			want:   []int{3, 4, 5},
		},
		{
			name:   "multiple tokens, independent hits",
			tokens: []string{"foo", "baz"},
			text:   "foo bar baz",
			want:   []int{0, 1, 2, 8, 9, 10},
		},
		{
			name:   "cross line",
			tokens: []string{"lo"},
			text:   "hello\nloom",
			want:   []int{3, 4, 6, 7},
		},
		{
			name:   "repeat occurrences",
			tokens: []string{"ab"},
			text:   "ababab",
			want:   []int{0, 1, 2, 3, 4, 5},
		},
		{
			name:   "overlapping tokens merge via sortedUnique",
			tokens: []string{"foo", "oob"},
			text:   "foobar",
			want:   []int{0, 1, 2, 3},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TokenSpansInText(tc.tokens, tc.text)
			if !slices.Equal(got, tc.want) {
				t.Errorf("TokenSpansInText(%v, %q) = %v, want %v", tc.tokens, tc.text, got, tc.want)
			}
		})
	}
}
