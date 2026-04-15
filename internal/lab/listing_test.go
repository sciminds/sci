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
			name: "mixed types from ls -1F",
			in: "data/\n" +
				"README.md\n" +
				"scripts/\n" +
				"link@\n" +
				"runme*\n",
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
			in:   "a/\n\nb\n\n",
			want: []Entry{
				{Name: "a", IsDir: true},
				{Name: "b"},
			},
		},
		{
			name: "dotfiles kept",
			in:   ".hidden\n.dotdir/\n",
			want: []Entry{
				{Name: ".hidden"},
				{Name: ".dotdir", IsDir: true},
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
	want := []string{"ssh", "scilab-alice", "ls", "-1FA", "--group-directories-first", "/labs/sciminds/data"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildBrowseLsArgs\n  got:  %#v\n  want: %#v", got, want)
	}
}
