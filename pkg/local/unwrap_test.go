package local

import "testing"

func TestUnwrapZoteroDiv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "standard wrapper",
			in:   `<div class="zotero-note znv1">inner content</div>`,
			want: "inner content",
		},
		{
			name: "no wrapper passthrough",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "wrapper with leading whitespace",
			in:   `  <div class="zotero-note znv1">content</div>`,
			want: "content",
		},
		{
			name: "wrapper with markdown pre tag",
			in: `<div class="zotero-note znv1"><pre>---
title: Test
---

# Heading</pre></div>`,
			want: `<pre>---
title: Test
---

# Heading</pre>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnwrapZoteroDiv(tt.in)
			if got != tt.want {
				t.Errorf("UnwrapZoteroDiv() = %q, want %q", got, tt.want)
			}
		})
	}
}
