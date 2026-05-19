package app

import "testing"

func TestPrettyPrintJSON(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{
			name:   "object",
			in:     `{"a":1,"b":2}`,
			want:   "{\n  \"a\": 1,\n  \"b\": 2\n}",
			wantOK: true,
		},
		{
			name:   "preserves key order",
			in:     `{"z":1,"a":2,"m":3}`,
			want:   "{\n  \"z\": 1,\n  \"a\": 2,\n  \"m\": 3\n}",
			wantOK: true,
		},
		{
			name:   "array",
			in:     `[1,2,3]`,
			want:   "[\n  1,\n  2,\n  3\n]",
			wantOK: true,
		},
		{
			name:   "nested",
			in:     `{"a":[1,2],"b":{"c":3}}`,
			want:   "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": {\n    \"c\": 3\n  }\n}",
			wantOK: true,
		},
		{
			name:   "duckdb interval shape",
			in:     `{"months":0,"days":0,"micros":3600000000}`,
			want:   "{\n  \"months\": 0,\n  \"days\": 0,\n  \"micros\": 3600000000\n}",
			wantOK: true,
		},
		{
			name:   "leading whitespace preserved as no-op detection only",
			in:     `  {"a":1}  `,
			want:   "{\n  \"a\": 1\n}",
			wantOK: true,
		},
		{
			name:   "plain string unchanged",
			in:     "Widget",
			want:   "Widget",
			wantOK: false,
		},
		{
			name:   "number unchanged",
			in:     "42",
			want:   "42",
			wantOK: false,
		},
		{
			name:   "empty unchanged",
			in:     "",
			want:   "",
			wantOK: false,
		},
		{
			name:   "whitespace unchanged",
			in:     "   ",
			want:   "   ",
			wantOK: false,
		},
		{
			name:   "malformed JSON unchanged",
			in:     `{not json}`,
			want:   `{not json}`,
			wantOK: false,
		},
		{
			name:   "empty object compact",
			in:     `{}`,
			want:   `{}`,
			wantOK: true,
		},
		{
			name:   "empty array compact",
			in:     `[]`,
			want:   `[]`,
			wantOK: true,
		},
		{
			name:   "string starting with brace not valid",
			in:     `{ bare text without quotes }`,
			want:   `{ bare text without quotes }`,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := prettyPrintJSON(tc.in)
			if got != tc.want {
				t.Errorf("prettyPrintJSON(%q)\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
			if ok != tc.wantOK {
				t.Errorf("prettyPrintJSON(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
		})
	}
}
