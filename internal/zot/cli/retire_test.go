package cli

import "testing"

func TestRewriteCommandFix(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		old  []string
		new  []string
		want string
	}{
		{
			name: "verb pair swapped, trailing args preserved",
			argv: []string{"sci", "zot", "notes", "add", "AAAA1111"},
			old:  []string{"notes", "add"},
			new:  []string{"content", "extract"},
			want: "sci zot content extract AAAA1111",
		},
		{
			name: "flags after the verb survive the rewrite",
			argv: []string{"sci", "zot", "--library", "personal", "notes", "delete", "--all", "--yes"},
			old:  []string{"notes", "delete"},
			new:  []string{"content", "drop"},
			want: "sci zot --library personal content drop --all --yes",
		},
		{
			name: "single-token command",
			argv: []string{"sci", "zot", "extract", "6R45EVSB", "--apply"},
			old:  []string{"extract"},
			new:  []string{"content", "extract"},
			want: "sci zot content extract 6R45EVSB --apply",
		},
		{
			name: "args needing quotes are quoted",
			argv: []string{"sci", "zot", "notes", "add", "a b"},
			old:  []string{"notes", "add"},
			new:  []string{"content", "extract"},
			want: "sci zot content extract 'a b'",
		},
		{
			name: "no zot in argv (go test binary) yields no fix",
			argv: []string{"/tmp/cli.test", "-test.run", "X"},
			old:  []string{"notes", "add"},
			new:  []string{"content", "extract"},
			want: "",
		},
		{
			name: "old verb absent yields no fix rather than a wrong one",
			argv: []string{"sci", "zot", "search", "foo"},
			old:  []string{"notes", "add"},
			new:  []string{"content", "extract"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteCommandFix(tt.argv, tt.old, tt.new); got != tt.want {
				t.Errorf("rewriteCommandFix() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A trailing flag goes at the end, not spliced in after the verb: `notes
// add` posted immediately while `content extract` dry-runs by default, so
// the Fix has to carry --apply to mean the same thing — and it should read
// the way the docs write it.
func TestRewriteCommandFix_TrailingFlagGoesLast(t *testing.T) {
	got := rewriteCommandFix(
		[]string{"sci", "zot", "--library", "personal", "notes", "add", "AAAA1111"},
		[]string{"notes", "add"}, []string{"content", "extract"}, "--apply")
	want := "sci zot --library personal content extract AAAA1111 --apply"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
