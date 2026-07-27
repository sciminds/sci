package notemd

// Tests pin down three things for the markdown → Zotero-HTML pipeline:
//
//  1. Common markdown shapes (bold, code, links) survive the round trip.
//  2. Hostile HTML is stripped by the sanitizer on BOTH paths (markdown
//     can smuggle raw HTML; --html passthrough is sanitized too).
//  3. The tags I actually used in the sciminds lit-review note are all
//     preserved — h1-h6, p, strong/em, code/pre, ol/ul/li, a, hr,
//     blockquote — so lab notes round-trip without silent content loss.

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML_basic(t *testing.T) {
	t.Parallel()
	got, err := MarkdownToHTML([]byte("**bold** and *italic*"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("missing <strong>: %q", got)
	}
	if !strings.Contains(got, "<em>italic</em>") {
		t.Errorf("missing <em>: %q", got)
	}
}

func TestMarkdownToHTML_fencedCode(t *testing.T) {
	t.Parallel()
	src := "```\nfoo()\nbar()\n```\n"
	got, err := MarkdownToHTML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	// goldmark emits <pre><code>...</code></pre>; exact whitespace varies, so
	// just assert both tags are present and the code body survives.
	if !strings.Contains(got, "<pre>") || !strings.Contains(got, "<code>") {
		t.Errorf("missing <pre><code>: %q", got)
	}
	if !strings.Contains(got, "foo()") || !strings.Contains(got, "bar()") {
		t.Errorf("code body dropped: %q", got)
	}
}

func TestMarkdownToHTML_linksPreserved(t *testing.T) {
	t.Parallel()
	got, err := MarkdownToHTML([]byte(`See [Dayan 1993](https://doi.org/10.1162/neco.1993.5.4.613).`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<a href="https://doi.org/10.1162/neco.1993.5.4.613"`) {
		t.Errorf("anchor lost: %q", got)
	}
	if !strings.Contains(got, "Dayan 1993") {
		t.Errorf("link text lost: %q", got)
	}
}

func TestMarkdownToHTML_sanitizesEmbeddedScript(t *testing.T) {
	t.Parallel()
	// Markdown permits raw HTML; the sanitizer must still strip <script>.
	got, err := MarkdownToHTML([]byte("hello\n\n<script>alert(1)</script>\n\nworld"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(got), "<script") {
		t.Errorf("<script> tag survived sanitization: %q", got)
	}
	if strings.Contains(got, "alert(1)") {
		// bluemonday's UGC policy strips <script> AND its contents.
		t.Errorf("<script> contents leaked: %q", got)
	}
}

func TestMarkdownToHTML_emptyInput(t *testing.T) {
	t.Parallel()
	got, err := MarkdownToHTML(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("nil input: got %q, want \"\"", got)
	}
	got, err = MarkdownToHTML([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("empty input: got %q, want \"\"", got)
	}
}

func TestSanitizeHTML_rejectsEventHandlers(t *testing.T) {
	t.Parallel()
	got := SanitizeHTML(`<a href="https://example.com" onclick="steal()">click</a>`)
	// Anchor + href survive; onclick stripped.
	if !strings.Contains(got, `href="https://example.com"`) {
		t.Errorf("href stripped: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "onclick") {
		t.Errorf("onclick survived: %q", got)
	}
}

func TestSanitizeHTML_preservesZoteroSafeTags(t *testing.T) {
	t.Parallel()
	// These are the tags I used in the sciminds lit-review note — each must
	// round-trip through SanitizeHTML unchanged in structure.
	src := `<h1>Title</h1><p>Body with <strong>bold</strong> and <em>italic</em> and <code>code</code>.</p>` +
		`<h2>Section</h2><ol><li>one</li><li>two</li></ol><ul><li>a</li></ul>` +
		`<pre><code>block</code></pre><a href="https://example.com">link</a><hr/>` +
		`<blockquote>quote</blockquote>`
	got := SanitizeHTML(src)
	must := []string{
		"<h1>", "<h2>", "<p>", "<strong>", "<em>", "<code>",
		"<ol>", "<ul>", "<li>", "<pre>", "<a ", "<hr", "<blockquote>",
	}
	for _, tag := range must {
		if !strings.Contains(got, tag) {
			t.Errorf("required tag %q missing from sanitized output:\n%s", tag, got)
		}
	}
}

func TestSanitizeHTML_empty(t *testing.T) {
	t.Parallel()
	if got := SanitizeHTML(""); got != "" {
		t.Errorf("empty input: got %q, want \"\"", got)
	}
}

// Lit-review summary notes cross-link to Zotero items via zotero://select/…
// URIs. bluemonday's UGCPolicy defaults to http/https/mailto only, which
// silently strips these anchors. The allow-list extension here is what makes
// "summary note with clickable refs" actually work.
func TestMarkdownToHTML_preservesZoteroLinks(t *testing.T) {
	t.Parallel()
	src := `See [Ho 2022](zotero://select/groups/6506098/items/B3FC5Y8C) for context.`
	got, err := MarkdownToHTML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `href="zotero://select/groups/6506098/items/B3FC5Y8C"`) {
		t.Errorf("zotero:// anchor stripped: %q", got)
	}
}

func TestSanitizeHTML_preservesZoteroLinks(t *testing.T) {
	t.Parallel()
	src := `<a href="zotero://select/library/items/ABC12345">link</a>`
	got := SanitizeHTML(src)
	if !strings.Contains(got, `href="zotero://select/library/items/ABC12345"`) {
		t.Errorf("zotero:// anchor stripped: %q", got)
	}
}

// Other custom schemes (javascript:, data:, file:) must still be stripped.
// Extending the allow-list for zotero shouldn't weaken the default policy.
func TestMarkdownToHTML_stripsForbiddenSchemes(t *testing.T) {
	t.Parallel()
	cases := []string{
		`[x](javascript:alert(1))`,
		`[x](data:text/html,<script>)`,
		`[x](file:///etc/passwd)`,
	}
	for _, src := range cases {
		got, err := MarkdownToHTML([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "<a href") {
			t.Errorf("forbidden scheme survived for %q: %q", src, got)
		}
	}
}

// --- HTML → markdown -------------------------------------------------------
//
// Without this direction Zotero is a write-only store: sci can put a lit note
// in and never cleanly get it back out. These pin the round trip for exactly
// the tag vocabulary the sanitizer policy allows, so what MarkdownToHTML can
// produce is what HTMLToMarkdown can recover.

func roundTrip(t *testing.T, md string) string {
	t.Helper()
	html, err := MarkdownToHTML([]byte(md))
	if err != nil {
		t.Fatal(err)
	}
	got, err := HTMLToMarkdown(html)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestHTMLToMarkdown_RoundTripsThePolicyVocabulary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		md   string
		want string
	}{
		{"heading", "## Central Thesis", "## Central Thesis"},
		{"bold", "**bold**", "**bold**"},
		{"italic", "*italic*", "*italic*"},
		{"inline code", "call `foo()` here", "`foo()`"},
		{"link", "[Dayan 1993](https://doi.org/10.1162/neco.1993.5.4.613)",
			"[Dayan 1993](https://doi.org/10.1162/neco.1993.5.4.613)"},
		{"bullet list", "- one\n- two", "- one"},
		{"ordered list", "1. first\n2. second", "1. first"},
		{"blockquote", "> quoted", "> quoted"},
		{"rule", "text\n\n---\n\nmore", "---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := roundTrip(t, tt.md); !strings.Contains(got, tt.want) {
				t.Errorf("round trip of %q = %q, want it to contain %q", tt.md, got, tt.want)
			}
		})
	}
}

func TestHTMLToMarkdown_FencedCodeSurvives(t *testing.T) {
	t.Parallel()
	got := roundTrip(t, "```\nfoo()\nbar()\n```\n")
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
