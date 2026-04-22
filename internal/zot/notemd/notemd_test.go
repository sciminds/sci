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
