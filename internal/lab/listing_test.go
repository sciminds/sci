package lab

import (
	"reflect"
	"testing"
)

func TestParseLsOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Entry
	}{
		{
			name: "empty",
			in:   "",
			want: []Entry{},
		},
		{
			name: "mixed types from ls -1FQ",
			in: "\"data\"/\n" +
				"\"README.md\"\n" +
				"\"scripts\"/\n" +
				"\"link\"@\n" +
				"\"runme\"*\n",
			want: []Entry{
				{Name: "data", IsDir: true},
				{Name: "README.md"},
				{Name: "scripts", IsDir: true},
				{Name: "link", IsLink: true},
				{Name: "runme"},
			},
		},
		{
			name: "trailing blank lines ignored",
			in:   "\"a\"/\n\n\"b\"\n\n",
			want: []Entry{
				{Name: "a", IsDir: true},
				{Name: "b"},
			},
		},
		{
			name: "dotfiles kept",
			in:   "\".hidden\"\n\".dotdir\"/\n",
			want: []Entry{
				{Name: ".hidden"},
				{Name: ".dotdir", IsDir: true},
			},
		},
		{
			name: "name with spaces",
			in:   "\"my data\"/\n\"two words\"\n",
			want: []Entry{
				{Name: "my data", IsDir: true},
				{Name: "two words"},
			},
		},
		{
			name: "name with newline stays one entry",
			// ls -Q escapes embedded newline as \n inside the quoted name.
			in: "\"weird\\nname\"\n\"normal\"\n",
			want: []Entry{
				{Name: "weird\nname"},
				{Name: "normal"},
			},
		},
		{
			name: "name with embedded quote",
			in:   "\"quote\\\"inside\"\n",
			want: []Entry{
				{Name: "quote\"inside"},
			},
		},
		{
			name: "malformed lines skipped",
			in:   "not_quoted\n\"valid\"\n",
			want: []Entry{
				{Name: "valid"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseLsOutput(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseLsOutput(%q)\n  got:  %#v\n  want: %#v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildBrowseLsArgs(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildBrowseLsArgs(cfg, "/labs/sciminds/data")
	want := []string{"ssh", "scilab-alice", "ls", "-1FQ", "--group-directories-first", "'/labs/sciminds/data'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildBrowseLsArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestBuildBrowseLsArgs_PathWithSpace(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildBrowseLsArgs(cfg, "/labs/x/my data")
	// Single-quoted path survives the remote login shell verbatim.
	want := []string{"ssh", "scilab-alice", "ls", "-1FQ", "--group-directories-first", "'/labs/x/my data'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildBrowseLsArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}

func TestBuildBrowseLsArgs_PathWithEmbeddedQuote(t *testing.T) {
	cfg := &Config{User: "alice"}
	got := BuildBrowseLsArgs(cfg, "/labs/it's/data")
	want := []string{"ssh", "scilab-alice", "ls", "-1FQ", "--group-directories-first", `'/labs/it'\''s/data'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildBrowseLsArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}
