package notemd

// Tests pin down the read path from Zotero's `note` field back to markdown:
//
//  1. The tag vocabulary Zotero's editor actually emits — h1-h6, p,
//     strong/em, code/pre, ol/ul/li, a, hr, blockquote — converts without
//     silent content loss.
//  2. A markdown note (the overwhelming majority) comes back byte-faithful,
//     because running it through the converter would destroy it.
//  3. IsHTMLNote tells those two shapes apart on the real library's inputs.

import (
	"strings"
	"testing"
)

// convert is HTMLToMarkdown with the error check inlined, for the table
// tests below.
func convert(t *testing.T, noteHTML string) string {
	t.Helper()
	got, err := HTMLToMarkdown(noteHTML)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestHTMLToMarkdown_CoversZoteroTagVocabulary walks the block and inline
// tags Zotero's own note editor emits. Each input is the note body as it is
// stored, so a converter regression shows up as a missing markdown
// construct rather than as a mangled fixture.
func TestHTMLToMarkdown_CoversZoteroTagVocabulary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
		want string
	}{
		{"heading", "<h2>Central Thesis</h2>", "## Central Thesis"},
		{"bold", "<p><strong>bold</strong></p>", "**bold**"},
		{"italic", "<p><em>italic</em></p>", "*italic*"},
		{"inline code", "<p>call <code>foo()</code> here</p>", "`foo()`"},
		{"link", `<p><a href="https://doi.org/10.1162/neco.1993.5.4.613">Dayan 1993</a></p>`,
			"[Dayan 1993](https://doi.org/10.1162/neco.1993.5.4.613)"},
		{"bullet list", "<ul><li>one</li><li>two</li></ul>", "- one"},
		{"ordered list", "<ol><li>first</li><li>second</li></ol>", "1. first"},
		{"blockquote", "<blockquote>quoted</blockquote>", "> quoted"},
		{"rule", "<p>text</p><hr/><p>more</p>", "---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := convert(t, tt.html); !strings.Contains(got, tt.want) {
				t.Errorf("HTMLToMarkdown(%q) = %q, want it to contain %q", tt.html, got, tt.want)
			}
		})
	}
}

func TestHTMLToMarkdown_FencedCodeSurvives(t *testing.T) {
	t.Parallel()
	got := convert(t, "<pre><code>foo()\nbar()\n</code></pre>")
	if !strings.Contains(got, "foo()") || !strings.Contains(got, "bar()") {
		t.Errorf("code body lost: %q", got)
	}
	if !strings.Contains(got, "```") {
		t.Errorf("fence lost: %q", got)
	}
}

// TestHTMLToMarkdown_StripsZoteroWrapper — every note Zotero stores is
// wrapped in its own div; that chrome must not surface as content.
func TestHTMLToMarkdown_StripsZoteroWrapper(t *testing.T) {
	t.Parallel()
	got, err := HTMLToMarkdown(`<div class="zotero-note znv1"><div data-schema-version="9">` +
		`<h1>Successor Representations</h1><p>The SR factorizes graph communicability.</p></div></div>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"zotero-note", "znv1", "data-schema-version", "<div"} {
		if strings.Contains(got, leak) {
			t.Errorf("wrapper leaked %q into markdown: %q", leak, got)
		}
	}
	if !strings.Contains(got, "# Successor Representations") {
		t.Errorf("heading lost: %q", got)
	}
}

// TestHTMLToMarkdown_PreservesZoteroLinks — zotero://select and zotero://note
// URIs are how a note points at a library item or another note. Phase 4/5
// build on them; losing them here would break the graph silently.
func TestHTMLToMarkdown_PreservesZoteroLinks(t *testing.T) {
	t.Parallel()
	got, err := HTMLToMarkdown(
		`<p>See <a href="zotero://select/library/items/XH7EKQTM">Whittington et al. 2022</a>.</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "zotero://select/library/items/XH7EKQTM") {
		t.Errorf("zotero URI lost: %q", got)
	}
	if !strings.Contains(got, "Whittington et al. 2022") {
		t.Errorf("link text lost: %q", got)
	}
}

// TestHTMLToMarkdown_EntitiesDecoded — "&amp;" is an ampersand to the reader.
func TestHTMLToMarkdown_EntitiesDecoded(t *testing.T) {
	t.Parallel()
	got, err := HTMLToMarkdown(`<p>communicability &amp; predictive maps</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "communicability & predictive maps") {
		t.Errorf("entity not decoded: %q", got)
	}
}

// TestHTMLToMarkdown_DoclingFrontmatterSurvives — extraction notes carry a
// YAML block as literal text at the top of the note. It has to come back
// verbatim or the note's provenance (zotero_key, hash) is lost.
func TestHTMLToMarkdown_DoclingFrontmatterSurvives(t *testing.T) {
	t.Parallel()
	got, err := HTMLToMarkdown("<div class=\"zotero-note znv1\">---\nzotero_key: AQGILY8X\n" +
		"title: \"Trait-names: A psycho-lexical study\"\nsource: docling (cached)\n---\n\n<p>Body.</p></div>")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"zotero_key: AQGILY8X", "source: docling (cached)"} {
		if !strings.Contains(got, want) {
			t.Errorf("frontmatter lost %q: %q", want, got)
		}
	}
}

func TestHTMLToMarkdown_EmptyIsEmpty(t *testing.T) {
	t.Parallel()
	got, err := HTMLToMarkdown("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestIsHTMLNote_RealWorldShapes pins the discriminator against the two
// shapes that actually occur in the library. Getting this wrong is not a
// cosmetic bug: 5,098 of 5,140 notes are the markdown shape, and running
// those through the HTML converter destroys their YAML provenance block.
func TestIsHTMLNote_RealWorldShapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		note string
		want bool
	}{
		{
			name: "docling extraction — markdown in a wrapper",
			note: "<div class=\"zotero-note znv1\">---\nzotero_key: 4XMAW6CX\n---\n\n## Heading\n",
			want: false,
		},
		{
			name: "sci-authored HTML note",
			note: `<div class="zotero-note znv1"><p>Accessed: 2026-4-9</p></div>`,
			want: true,
		},
		{
			name: "Zotero editor's nested schema div",
			note: `<div class="zotero-note znv1"><div data-schema-version="9"><h1>Title</h1></div></div>`,
			want: true,
		},
		{
			// Docling markdown embeds HTML tables mid-document. The note is
			// still markdown; only what OPENS the body decides.
			name: "markdown that embeds an HTML table later on",
			note: "<div class=\"zotero-note znv1\">---\nzotero_key: CJYIIEDJ\n---\n\n## Results\n\n<table><tr><td>1</td></tr></table>\n",
			want: false,
		},
		{
			name: "bare markdown, no wrapper at all",
			note: "# Just markdown\n\nBody.",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsHTMLNote(tt.note); got != tt.want {
				t.Errorf("IsHTMLNote() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHTMLToMarkdown_MarkdownNoteIsByteFaithful — the markdown path must not
// reflow, escape, or re-wrap anything. Only the wrapper and entities go.
func TestHTMLToMarkdown_MarkdownNoteIsByteFaithful(t *testing.T) {
	t.Parallel()
	body := "---\nzotero_key: 4XMAW6CX\ntitle: \"Thinking-fast, slow &amp; artificial\"\n---\n\n" +
		"## Heading\n\nText with snake_case_words and a [link](https://example.org).\n"
	got, err := HTMLToMarkdown(`<div class="zotero-note znv1">` + body + `</div>`)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(strings.Replace(body, "&amp;", "&", 1))
	if got != want {
		t.Errorf("markdown note was rewritten:\n got: %q\nwant: %q", got, want)
	}
	// The specific regressions: newlines collapsing and underscore escaping.
	if !strings.Contains(got, "\nzotero_key: 4XMAW6CX\n") {
		t.Error("YAML frontmatter lost its line breaks")
	}
	if strings.Contains(got, `\_`) {
		t.Error("underscores were backslash-escaped")
	}
}

// TestIsHTMLNote_InlineOnlyHTMLIsStillHTML — a body with no block tag but
// obvious inline markup must not be mistaken for markdown and passed through
// verbatim; that is how "<b>bold</b>" reached the terminal as raw tags.
func TestIsHTMLNote_InlineOnlyHTMLIsStillHTML(t *testing.T) {
	t.Parallel()
	for _, note := range []string{
		"<b>bold</b> and <i>italic</i>",
		`Some text with an <a href="https://example.org">inline link</a>.`,
		"plain prose with no markup at all",
	} {
		if !IsHTMLNote(note) {
			t.Errorf("IsHTMLNote(%q) = false, want true", note)
		}
	}
}
