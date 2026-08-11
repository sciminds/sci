// markdown template — a manuscript written in pure Markdown, configured by
// typst.yml, typeset by this file. Self-contained: no @local/sciminds import,
// so it compiles anywhere Typst runs and kit edits can't reach into a paper.
//
//   typst watch main.typ        # ~ms rebuilds, .md and .yaml edits included
//   just preview .              # browser preview via tinymist
//
// The prose files are CommonMark, which cmarker renders natively — including
// [@key] citations (they compile to Typst refs, which resolve against the
// bibliography), [@fig-x]/[@tbl-x] cross-references, and [text](#heading)
// links against auto-generated heading slugs. Only two things CommonMark
// cannot say are rewritten before cmarker sees them (the "dialect layer"):
// a `: caption {#tbl-id}` line under a table, and narrative `@key` citations.
// Figure labels are derived from image filenames, so they need no syntax.

#import "@preview/cmarker:0.1.10"
#import "@preview/mitex:0.2.7": mitex
#import "@preview/equate:0.3.3": equate
#import "@preview/subpar:0.2.2"
#import "@preview/wordometer:0.1.5": word-count, total-words
#import "@preview/zebraw:0.6.3": zebraw

// ── Configuration ────────────────────────────────────────────────────
// Everything user-facing lives in typst.yml; these are the fallbacks that
// make every key optional there.

#let cfg = (
  title: "Untitled",
  short_title: none,
  authors: (),
  affiliations: (),
  abstract: none,
  keywords: (),
  content: (),
  bibliography: none,
  two_column: false,
  draft: true,
  font: auto, // auto → Libertinus Serif (Typst's default, always available)
  font_size: 11,
  indent_paragraphs: true,
  numbered_sections: false,
  line_numbers: "auto",
  booktabs_tables: true,
  align_decimals: false,
  number_equations: false,
  code_style: "plain",
  show_dois: true,
  show_correspondence: true,
  word_count: true,
  citation_style: "american-psychological-association",
) + yaml("typst.yml")

#let two-col = cfg.two_column
#let draft = cfg.draft
#let accent = rgb("#2a5599")
#let numbered-lines = cfg.line_numbers == true or (cfg.line_numbers == "auto" and draft)

#assert(
  cfg.content.len() > 0,
  message: "typst.yml: `content:` must list at least one markdown file",
)

// ── Dialect layer ────────────────────────────────────────────────────
// The only rewriting in the pipeline. Both rewrites use Quarto's own syntax,
// so the .md files remain valid Quarto Markdown.

// A Typst string literal, for text pasted into generated source. Newlines and
// `>` are emitted as escapes so the literal stays on one line and can never
// contain `-->`: these strings travel inside `<!--raw-typst … -->` comments,
// and their content may itself carry nested raw-typst comments (phantom
// digits, rewritten citations) whose closers would otherwise terminate the
// outer comment mid-string.
#let typst-str(s) = {
  let escaped = s.replace("\\", "\\\\").replace("\"", "\\\"").replace("\n", "\\n").replace(">", "\\u{3e}")
  "\"" + escaped + "\""
}

// figures/my behavior_plot.svg → fig-my-behavior-plot. The filename IS the
// figure's reference label, so distinct filenames are load-bearing.
#let filename-slug(path) = {
  let base = path.split("/").last()
  let stem = if base.contains(".") { base.split(".").slice(0, -1).join(".") } else { base }
  lower(stem).replace(regex("[^a-z0-9]+"), "-").trim("-")
}

// Bare `@key` is a narrative citation — "Chang et al. (2021) showed…" — unless
// its prefix marks it as a cross-reference. `[@key]` is untouched: cmarker
// handles it natively. The key must end in a word character, so sentence
// punctuation after "@key." isn't swallowed into it. The leading character is
// captured and replayed because Typst's regex engine has no look-behind;
// excluding word chars keeps it off email addresses, excluding `\` gives an
// escape hatch (`\@handle` renders as a literal @handle).
#let cite-key = "[A-Za-z][A-Za-z0-9_:.#$%&+?<>~/-]*[A-Za-z0-9_]|[A-Za-z]"
#let crossref-prefixes = ("fig-", "tbl-", "eq-", "sec-")

#let rewrite-narrative-citations(s) = s.replace(
  regex("(^|[^\\[\\w@\\\\])@(" + cite-key + ")"),
  m => {
    let key = m.captures.at(1)
    let call = if crossref-prefixes.any(p => key.starts-with(p)) {
      "#ref(<" + key + ">)"
    } else {
      "#cite(<" + key + ">, form: \"prose\")"
    }
    m.captures.at(0) + "<!--raw-typst " + call + "-->"
  },
)

// A `: caption` line directly under a pipe table turns it into a numbered,
// referenceable figure; `{#tbl-id}` at the end of the caption names it. The
// table's own markdown is handed to #md-table (below) as a string and rendered
// there, inside the figure.
#let rewrite-table-captions(s) = s.replace(
  regex("(?m)((?:^\\|[^\n]*\n)+)(?:[ \t]*\n)*:[ \t]*([^\n]+)$"),
  m => {
    let table-md = m.captures.at(0)
    let caption = m.captures.at(1).trim()
    let id = ""
    let idm = caption.match(regex("[ \t]*\\{#([A-Za-z0-9_-]+)\\}$"))
    if idm != none {
      id = "<" + idm.captures.at(0) + ">"
      caption = caption.slice(0, idm.start).trim()
    }
    "<!--raw-typst #md-table(" + typst-str(table-md) + ", caption: " + typst-str(caption) + ")" + id + "-->\n"
  },
)

// Decimal alignment, done at the string level: numeric cells shallower than
// their column's deepest fraction are padded with *phantom* digits — `#hide[…]`
// renders invisibly at exactly the glyph's width, so with tabular figures the
// decimal points land on one vertical line. Numbers render exactly as typed
// (`.31` stays `.31`); no package reformats them. The whole cell is wrapped in
// one raw-typst comment with number and phantom glued together: emitted after
// markdown text instead, the phantom would pick up a leading space that Typst
// trims from unpadded cells but keeps before glyphs — a one-space misalignment.
#let numeric-cell = regex("^[+\-−]?[0-9]*\.?[0-9]+$")
#let delimiter-cell = regex("^:?-{3,}:?$")

#let pad-decimal-cells(tbl) = {
  let rows = tbl.trim("\n", at: end).split("\n").map(l => {
    l.trim().trim("|", at: start).trim("|", at: end).split("|").map(c => c.trim())
  })
  let delim = rows.position(r => r.len() > 0 and r.all(c => c.match(delimiter-cell) != none))
  if delim == none { return tbl }
  let body = range(rows.len()).filter(i => i > delim)
  for col in range(rows.at(delim).len()) {
    let frac(cell) = if cell.contains(".") { cell.split(".").last().len() } else { 0 }
    let numeric = body.filter(i => {
      col < rows.at(i).len() and rows.at(i).at(col).match(numeric-cell) != none
    })
    let fracs = numeric.map(i => frac(rows.at(i).at(col)))
    if fracs.len() == 0 or calc.max(..fracs) == calc.min(..fracs) { continue }
    let deepest = calc.max(..fracs)
    // A column being decimal-aligned is implicitly right-anchored; force it
    // unless the author already chose an alignment.
    if not rows.at(delim).at(col).contains(":") { rows.at(delim).at(col) = "---:" }
    for i in numeric {
      let cell = rows.at(i).at(col)
      let missing = deepest - frac(cell)
      let phantom = if frac(cell) == 0 { "." } else { "" } + "0" * missing
      if phantom != "" {
        rows.at(i).at(col) = "<!--raw-typst " + cell + "#hide[" + phantom + "]-->"
      }
    }
  }
  rows.map(r => "| " + r.join(" | ") + " |").join("\n") + "\n"
}

#let rewrite-decimal-tables(s) = s.replace(regex("(?m)(?:^\\|[^\n]*\n)+"), m => pad-decimal-cells(m.text))

// `![caption](fig.svg){width=45% #fig-custom}` — Quarto-style attributes on an
// image: `width=`/`height=` size it (the figure stays centered), `#id`
// overrides the filename-derived label.
#let parse-length(v) = {
  for (suffix, unit) in ("%": 1%, "in": 1in, "cm": 1cm, "mm": 1mm, "pt": 1pt, "em": 1em) {
    if v.ends-with(suffix) { return float(v.trim(suffix, at: end)) * unit }
  }
  none
}

#let attr-of(attrs, name) = {
  let m = attrs.match(regex(name + "=([^ \t}]+)"))
  if m != none { m.captures.at(0) } else { none }
}

#let id-of(attrs) = {
  let m = attrs.match(regex("#([A-Za-z0-9_-]+)"))
  if m != none { m.captures.at(0) } else { none }
}

#let image-with-attrs = regex("!\\[([^\\]]*)\\]\\(([^)\\s]+)\\)(?:\\{([^}\n]*)\\})?")

#let rewrite-image-attrs(s) = s.replace(
  regex("!\\[([^\\]]*)\\]\\(([^)\\s]+)\\)\\{([^}\n]*)\\}"),
  m => {
    let (alt, src, attrs) = (m.captures.at(0), m.captures.at(1), m.captures.at(2))
    let width = attr-of(attrs, "width")
    let id = id-of(attrs)
    let width-arg = if width != none { ", width: " + width.replace("%", "*1%") } else { "" }
    let name = if id != none { id } else { "fig-" + filename-slug(src) }
    "<!--raw-typst #md-figure(" + typst-str(src) + ", alt: " + typst-str(alt) + width-arg + ")<" + name + ">-->"
  },
)

// Panel figures, Quarto's own syntax: a fenced div of 2-4 images becomes one
// multi-panel figure (side-by-side, stacked, or a quadrant via layout-ncol),
// numbered 1a/1b/…, each panel and the whole referenceable.
//
//   ::: {#fig-panels layout-ncol=2}
//   ![Panel caption.](figures/a.svg)
//   ![Panel caption.](figures/b.svg)
//
//   Caption for the whole figure.
//   :::
#let rewrite-figure-groups(s) = s.replace(
  regex("(?ms)^:{3,}[ \t]*\\{([^}\n]*)\\}[ \t]*$\n(.*?)\n:{3,}[ \t]*$"),
  m => {
    let (attrs, body) = (m.captures.at(0), m.captures.at(1))
    let images = body.matches(image-with-attrs)
    if images.len() == 0 { return m.text } // not a figure group; leave untouched
    let ncol = attr-of(attrs, "layout-ncol")
    let id = id-of(attrs)
    let caption = body.split("\n").filter(l => not l.trim().starts-with("![")).join("\n").trim()
    let panels = images.map(im => {
      let iattrs = if im.captures.at(2) != none { im.captures.at(2) } else { "" }
      let pid = id-of(iattrs)
      let name = if pid != none { pid } else { "fig-" + filename-slug(im.captures.at(1)) }
      "(src: " + typst-str(im.captures.at(1)) + ", alt: " + typst-str(im.captures.at(0)) + ", id: " + typst-str(name) + ")"
    })
    let ncol-arg = if ncol != none { ncol } else { "2" }
    let group-label = if id != none { ", label-name: " + typst-str(id) } else { "" }
    "<!--raw-typst #md-panels((" + panels.join(", ") + ",), ncol: " + ncol-arg + ", caption: " + typst-str(caption) + group-label + ")-->\n"
  },
)

// Rewrites must not fire inside code — `@decorator` in a Python listing or an
// inline span is not a citation. Code regions (``` fences of any length, then
// inline spans) are matched and passed through untouched; only the text
// between them is transformed. (Typst's regex has no back-references, so
// fences are matched by length class rather than paired exactly.)
#let code-region = regex("(?ms)^`{3,}[^\n]*$.*?^`{3,}[ \t]*$|`[^`\n]+`")

#let outside-code(s, transform) = {
  let out = ""
  let cursor = 0
  for m in s.matches(code-region) {
    out += transform(s.slice(cursor, m.start)) + m.text
    cursor = m.end
  }
  out + transform(s.slice(cursor))
}

// Order is load-bearing: citation injection must come first (so any comment it
// plants is safely escaped by whichever later pass stringifies that text), and
// table captions must come last (they capture cells that the decimal pass has
// already padded).
#let prepare(s) = outside-code(s, x => {
  let x = rewrite-narrative-citations(x)
  let x = rewrite-figure-groups(x)
  let x = rewrite-image-attrs(x)
  let x = if cfg.align_decimals { rewrite-decimal-tables(x) } else { x }
  rewrite-table-captions(x)
})

// ── Markdown rendering ───────────────────────────────────────────────

// `![caption](figures/behavior.svg)` → a numbered figure labelled
// <fig-behavior>: alt text becomes the caption (rendered as markdown, so
// emphasis and math work inside it), the filename becomes the label. An empty
// alt means the author wanted a plain, unnumbered image.
#let render-snippet(s) = cmarker.render(s, math: mitex)

#let md-image(src, alt: none, ..rest) = {
  let img = image(src, ..rest)
  if alt == none or alt == "" { img } else {
    [#figure(img, caption: render-snippet(alt)) #label("fig-" + filename-slug(src))]
  }
}

// A captioned table, emitted by the dialect layer above.
#let md-table(table-md, caption: none) = figure(
  render-snippet(table-md),
  caption: if caption != none { render-snippet(caption) },
  kind: table,
)

// A sized figure, emitted by the image-attribute rewrite. Its label is
// attached by the rewrite, in markup, right after the call.
#let md-figure(src, alt: none, width: auto) = {
  let img = image(src, width: width)
  if alt == none or alt == "" { img } else { figure(img, caption: render-snippet(alt)) }
}

// A multi-panel figure, emitted by the figure-group rewrite. subpar numbers
// the panels 1a/1b/… and makes both the whole and each panel referenceable.
#let md-panels(panels, ncol: 2, caption: none, label-name: none) = {
  let args = ()
  for p in panels {
    args.push(figure(
      image(p.src, width: 100%),
      caption: if p.alt != "" { render-snippet(p.alt) },
    ))
    args.push(label(p.id))
  }
  subpar.grid(
    ..args,
    columns: (1fr,) * ncol,
    caption: if caption != "" { render-snippet(caption) },
    label: if label-name != none { label(label-name) },
    // Re-emit subcaptions as plain content: this consumes the caption element,
    // so the document-level rule that bolds "Figure N:" never sees them and
    // "(a)" stays unbolded.
    show-sub-caption: (num, it) => [#num #it.body],
  )
}

// CommonMark fence content ends with a newline, which line-numbering styles
// would render as a phantom empty final line — trim it before Typst sees it.
#let md-raw(body, block: false, lang: none) = raw(body.trim("\n", at: end), block: block, lang: lang)

#let body = cmarker.render(
  prepare(cfg.content.map(f => read(f)).join("\n\n")),
  math: mitex,
  h1-level: 1,
  heading-labels: "github", // "## My Heading" is linkable as [](#my-heading)
  task-list-marker: done => if done [☑] else [☐],
  scope: (image: md-image, md-table: md-table, md-figure: md-figure, md-panels: md-panels, raw: md-raw),
)

// ── Page & typography ────────────────────────────────────────────────

#set document(
  title: cfg.title,
  author: cfg.authors.map(a => a.name),
)
#set page(
  paper: "us-letter",
  margin: if two-col { 1in } else { (x: 1.15in, y: 1in) },
  columns: if two-col { 2 } else { 1 },
  header: context {
    if counter(page).get().first() > 1 {
      set text(size: 9pt, fill: luma(90))
      emph(if cfg.short_title != none { cfg.short_title } else { cfg.title })
      h(1fr)
      if draft { [DRAFT #sym.dot.c #datetime.today().display("[month repr:long] [day], [year]")] }
    }
  },
  footer: context {
    set text(size: 9pt, fill: luma(90))
    h(1fr)
    counter(page).display("1")
    h(1fr)
    if draft and cfg.word_count {
      place(right + horizon, text(size: 8pt)[#total-words words])
    }
  },
)
#set text(
  font: if cfg.font == auto { "Libertinus Serif" } else { cfg.font },
  size: cfg.font_size * 1pt,
  lang: "en",
)
#set par(
  justify: true,
  leading: 0.65em,
  // Indent style separates paragraphs by indent alone (spacing == leading);
  // `all: false` keeps the first paragraph after a heading flush, the part the
  // old template got wrong.
  spacing: if cfg.indent_paragraphs { 0.65em } else { 1.1em },
  first-line-indent: if cfg.indent_paragraphs { (amount: 1.4em, all: false) } else { 0pt },
)
#set par.line(numbering: if numbered-lines {
  n => text(size: 7pt, fill: luma(140), str(n))
} else { none })
#show link: set text(fill: accent)

// Headings — unnumbered by default (psych/neuro convention).
#set heading(numbering: if cfg.numbered_sections { "1.1" } else { none })
#show heading: set par(justify: false)
#show heading.where(level: 1): it => block(above: 1.8em, below: 1em, text(1.15em, weight: "bold", it))
#show heading.where(level: 2): it => block(above: 1.5em, below: 0.9em, text(1.0em, weight: "bold", it))
#show heading.where(level: 3): it => block(above: 1.3em, below: 0.8em, text(0.95em, weight: "bold", style: "italic", it))

// ── Tables ───────────────────────────────────────────────────────────

#show table.cell.where(y: 0): it => if cfg.booktabs_tables { strong(it) } else { it }
#set table(
  stroke: if cfg.booktabs_tables { (_, y) => (top: if y == 1 { 0.5pt }) } else { 1pt + black },
  inset: if cfg.booktabs_tables { (x: 6pt, y: 4pt) } else { 5pt },
)
// Tables always sit centered — bare ones here; captioned ones are centered by
// their enclosing figure anyway, which is why this isn't worth a config flag.
// Tabular figures make every digit one width, which the phantom-digit decimal
// alignment relies on (and looks better in tables regardless).
#show table: set text(number-width: "tabular")
#show table: it => align(center, if cfg.booktabs_tables {
  block(stroke: (top: 0.7pt, bottom: 0.7pt), inset: (y: 3pt), above: 1.4em, below: 1.4em, it)
} else { it })

// ── Figures ──────────────────────────────────────────────────────────

#show figure: set block(above: 1.6em, below: 1.6em)
// "Figure N:" / "Table N:" lead their captions in bold. Panel subcaptions
// ("(a) …") are exempt — subpar re-emits them as plain content above.
#show figure.caption: it => if it.numbering == none { it } else {
  context {
    strong[#it.supplement #it.counter.display(it.numbering)#it.separator]
    it.body
  }
}
#show figure.caption: set text(size: 0.88em)
#show figure.caption: set par(leading: 0.55em)
#show figure.caption: set align(left)
#show figure.where(kind: table): set figure.caption(position: top)

// ── Equations, code, word count ──────────────────────────────────────

#set math.equation(numbering: if cfg.number_equations { "(1)" } else { none })
#show math.equation.where(block: true): set block(above: 1.4em, below: 1.4em)
#show: if cfg.number_equations { equate.with(breakable: true, sub-numbering: true) } else { it => it }

#show raw: set text(size: 0.92em)
#show: if cfg.code_style == "zebra" { zebraw.with(lang: false) } else { it => it }
// Both styles get breathing room; plain additionally gets its own panel.
#show raw.where(block: true): it => if cfg.code_style == "plain" {
  block(width: 100%, fill: luma(248), inset: 8pt, radius: 3pt, above: 1.4em, below: 1.4em, it)
} else {
  block(above: 1.4em, below: 1.4em, it)
}

#show: word-count.with(exclude: (heading, figure.caption, raw.where(block: true), bibliography))

// ── Front matter ─────────────────────────────────────────────────────

#let title-block = align(center, {
  block(text(1.55em, weight: "bold", par(justify: false, leading: 0.5em, cfg.title)))
  v(0.6em)
  let author-line = cfg.authors.map(a => {
    let marks = a.at("affiliations", default: ()).map(str).join(",")
    let star = if a.at("corresponding", default: false) and cfg.show_correspondence { "*" } else { "" }
    box[#a.name#super[#marks#star]]
  })
  text(1.05em, author-line.join(", ", last: ", and "))
  v(0.3em)
  block({
    set text(0.9em, fill: luma(60))
    set par(leading: 0.6em)
    for aff in cfg.affiliations {
      block(spacing: 0.55em)[#super[#str(aff.id)]#aff.name]
    }
    let corr = if cfg.show_correspondence {
      cfg.authors.filter(a => a.at("corresponding", default: false))
    } else { () }
    if corr.len() > 0 {
      v(0.5em)
      block[\*Correspondence: #corr.map(a => link("mailto:" + a.email, a.email)).join(", ")]
    }
  })
})

#let abstract-block = if cfg.abstract == none { none } else {
  block(width: 100%, inset: (x: 2.5em), {
    set par(first-line-indent: 0pt)
    v(1.2em)
    align(center, text(weight: "bold", size: 0.95em, "Abstract"))
    v(0.5em)
    set text(size: 0.95em)
    render-snippet(prepare(cfg.abstract))
    if cfg.keywords.len() > 0 {
      v(0.8em)
      text(size: 0.9em)[*Keywords:* #cfg.keywords.join(", ")]
    }
  })
}

// In two columns the front matter escapes the column flow and floats
// full-width above them.
#let front-matter = {
  title-block
  if abstract-block != none { abstract-block }
}
#if two-col {
  place(top, float: true, scope: "parent", clearance: 1.4em, front-matter)
} else {
  front-matter
  v(1.5em)
}

// ── Body & references ────────────────────────────────────────────────

#body

#if cfg.bibliography != none {
  set std.bibliography(title: "References", style: cfg.citation_style)
  show std.bibliography: set par(first-line-indent: 0pt, hanging-indent: 1.5em)
  // Scoped to this block, so a doi.org link in running text is untouched.
  show regex("https?://doi\\.org/\\S+"): it => if cfg.show_dois { it } else { none }
  std.bibliography(cfg.bibliography)
}
