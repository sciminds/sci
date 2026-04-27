// sci-preprint — arkheion-inspired preprint template for MyST + Typst.
// Single-column, New Computer Modern, 2pt title rules, superscript-keyed authors.

#let sci-preprint(
  title: "",
  subtitle: none,
  authors: (),
  affiliations: (),
  date: none,
  abstract: none,
  keypoints: none,
  acknowledgements: none,
  data-availability: none,
  keywords: (),
  heading-numbering: "1.1",
  citation-style: "nature",
  bibliography-file: none,
  // Submission options
  line-numbers: true,
  line-number-scope: "document",
  anonymize: false,
  title-page: false,
  abstract-page: false,
  body,
) = {
  // ── Document & page ────────────────────────────────────────────────
  set document(
    author: if anonymize { () } else { authors.map(a => a.name) },
    title: title,
  )
  set page(
    paper: "us-letter",
    margin: (left: 25mm, right: 25mm, top: 25mm, bottom: 30mm),
    numbering: "1",
    number-align: center,
  )
  set text(
    font: "New Computer Modern",
    size: 11pt,
    lang: "en",
  )
  set par(justify: true, leading: 0.65em, first-line-indent: 0pt)
  show raw: set text(font: "DejaVu Sans Mono", size: 0.92em)
  show math.equation: set text(weight: 400)
  set math.equation(numbering: "(1)")
  show bibliography: set heading(level: 2)

  // ── Headings ───────────────────────────────────────────────────────
  // MyST's Typst export demotes markdown `#` to Typst `==` (since the
  // document already has a title), so the "first visible section" is
  // level 2. Drop the level-1 counter from the numbering pattern.
  let hnum = if heading-numbering == "" or heading-numbering == none {
    none
  } else {
    (..nums) => {
      let n = nums.pos()
      if n.len() <= 1 { [] } else { numbering(heading-numbering, ..n.slice(1)) }
    }
  }
  set heading(numbering: hnum)
  show heading: it => {
    if it.level == 1 {
      pad(bottom: 10pt, it)
    } else if it.level == 2 {
      pad(bottom: 8pt, it)
    } else if it.level > 3 {
      text(11pt, weight: "bold", it.body + ". ")
    } else {
      it
    }
  }

  // ── Helpers ────────────────────────────────────────────────────────
  let to-string(x) = if type(x) == str { x } else if x == none { "" } else { str(x) }
  let author-aff-ids(author) = {
    if "affiliations" not in author or author.affiliations == none { return () }
    to-string(author.affiliations)
      .split(",")
      .map(s => s.trim())
      .filter(s => s != "")
  }
  let join-authors(items) = {
    let n = items.len()
    if n == 0 { [] }
    else if n == 1 { items.at(0) }
    else if n == 2 { items.at(0) + [ and ] + items.at(1) }
    else {
      items.slice(0, n - 1).join(", ") + [, and ] + items.at(n - 1)
    }
  }
  let is-corresponding(a) = a.at("corresponding", default: false) == true

  // ── Title block (framed by 2pt rules) ──────────────────────────────
  line(length: 100%, stroke: 2pt)
  pad(
    top: 4pt,
    bottom: 4pt,
    align(center)[
      #block(text(weight: 500, 1.75em, title))
      #if subtitle != none {
        block(text(weight: 400, 1.15em, style: "italic", subtitle))
      }
      #v(1em, weak: true)
    ],
  )
  line(length: 100%, stroke: 2pt)

  // ── Authors (flat comma-separated, superscript affiliation keys) ───
  if anonymize {
    v(1.2em)
    align(center, text(style: "italic", 0.95em,
      [Author names and affiliations withheld for peer review]))
  } else if authors.len() > 0 {
    v(0.8em)
    align(center)[
      #set par(justify: false, leading: 0.7em)
      #{
        let parts = authors.map(author => {
          let name-part = if "orcid" in author {
            link("https://orcid.org/" + author.orcid)[
              #text(weight: "bold", author.name)
              #h(2pt)
              #box(baseline: -1pt, image("orcid.svg", width: 8pt))
            ]
          } else {
            text(weight: "bold", author.name)
          }
          let ids = author-aff-ids(author)
          let sup-keys = ids
          if is-corresponding(author) { sup-keys = sup-keys + ("†",) }
          if sup-keys.len() > 0 {
            name-part + super(sup-keys.join(","))
          } else {
            name-part
          }
        })
        join-authors(parts)
      }
    ]

    // Affiliations list (numbered lines, centered)
    if affiliations.len() > 0 {
      v(0.4em)
      align(center)[
        #set text(0.9em)
        #set par(leading: 0.5em)
        #{
          let lines = affiliations.map(aff => {
            super(to-string(aff.id)) + h(2pt) + aff.name
          })
          lines.join(linebreak())
        }
      ]
    }

    // Corresponding author(s) line
    let corr = authors.filter(is-corresponding)
    if corr.len() > 0 {
      v(0.4em)
      align(center)[
        #set text(0.9em)
        #set par(leading: 0.5em)
        #super("†") #h(2pt)
        #if corr.len() == 1 [Corresponding author:] else [Corresponding authors:]
        #h(0.3em)
        #{
          let entries = corr.map(a => {
            if "email" in a and a.email != none and a.email != "" {
              a.name + h(0.3em) + text(0.95em)[(#link("mailto:" + a.email, a.email))]
            } else {
              a.name
            }
          })
          entries.join(", ", last: ", ")
        }
      ]
    }
  }

  // ── Date ───────────────────────────────────────────────────────────
  if date != none {
    v(0.5em)
    align(center, text(0.95em, date))
  }

  // ── Key Points ─────────────────────────────────────────────────────
  if keypoints != none {
    v(0.8em)
    pad(x: 2em)[
      #text(0.8em, weight: "bold", smallcaps("Highlights"))
      #set text(0.95em)
      #keypoints
    ]
  }

  // ── Optional page break before abstract ───────────────────────────
  if title-page or abstract-page {
    pagebreak(weak: true)
  }

  // ── Abstract ───────────────────────────────────────────────────────
  if abstract != none {
    pad(
      x: 3em,
      top: 0.8em,
      bottom: 0.4em,
      align(center)[
        #heading(
          outlined: false,
          numbering: none,
          text(0.85em, smallcaps[Abstract]),
        )
        #set par(justify: true)
        #set text(hyphenate: false)
        #align(left, abstract)
      ],
    )
  }

  // ── Keywords ───────────────────────────────────────────────────────
  if keywords.len() > 0 {
    pad(x: 3em)[
      *_Keywords_* #h(0.3cm)
      #keywords.map(k => to-string(k)).join(" · ")
    ]
  }

  // ── Optional page break before body ───────────────────────────────
  if abstract-page {
    pagebreak(weak: true)
  }

  v(1.2em)

  // ── Line numbers turn on here; title/abstract above stay unnumbered
  set par.line(numbering: "1", numbering-scope: line-number-scope) if line-numbers

  // ── Body ───────────────────────────────────────────────────────────
  set par(justify: true)
  set text(hyphenate: false)
  body

  // ── Acknowledgements (unnumbered heading) ──────────────────────────
  if acknowledgements != none {
    v(1em)
    heading(level: 3, numbering: none, outlined: false)[Acknowledgements]
    acknowledgements
  }

  // ── Data availability (unnumbered heading) ─────────────────────────
  if data-availability != none {
    v(1em)
    heading(level: 3, numbering: none, outlined: false)[Data Availability]
    data-availability
  }

  // ── Bibliography ───────────────────────────────────────────────────
  if bibliography-file != none {
    v(2em)
    line(length: 100%, stroke: 0.5pt)
    bibliography(
      bibliography-file,
      title: none,
      style: citation-style,
    )
  }
}

#show: sci-preprint.with(
  title: "[-doc.title-]",
[# if doc.subtitle #]
  subtitle: "[-doc.subtitle-]",
[# endif #]
  authors: (
[# for author in doc.authors #]
    (
      name: "[-author.name-]",
[# if author.email #]
      email: "[-author.email-]",
[# endif #]
[# if author.orcid #]
      orcid: "[-author.orcid-]",
[# endif #]
[# if author.corresponding #]
      corresponding: true,
[# endif #]
[# if author.affiliations #]
      affiliations: "[#- for aff in author.affiliations -#][-aff.index-][#- if not loop.last -#],[#- endif -#][#- endfor -#]",
[# endif #]
    ),
[# endfor #]
  ),
  affiliations: (
[# for aff in doc.affiliations #]
    (
      id: "[-aff.index-]",
      name: "[-aff.name-]",
    ),
[# endfor #]
  ),
[# if doc.date #]
  date: datetime(
    year: [-doc.date.year-],
    month: [-doc.date.month-],
    day: [-doc.date.day-],
  ).display(),
[# endif #]
[# if parts.abstract #]
  abstract: [
[-parts.abstract-]
  ],
[# endif #]
[# if parts.keypoints #]
  keypoints: [
[-parts.keypoints-]
  ],
[# endif #]
[# if parts.acknowledgements #]
  acknowledgements: [
[-parts.acknowledgements-]
  ],
[# endif #]
[# if parts.data_availability #]
  data-availability: [
[-parts.data_availability-]
  ],
[# endif #]
[# if doc.keywords #]
  keywords: (
    [#- for keyword in doc.keywords -#]"[-keyword-]",[#- endfor -#]
  ),
[# endif #]
[# if options.heading_numbering #]
  heading-numbering: "[-options.heading_numbering-]",
[# endif #]
[# if options.citation_style #]
  citation-style: "[-options.citation_style-]",
[# endif #]
  line-numbers: [# if options.line_numbers #]true[# else #]false[# endif #],
  line-number-scope: "[-options.line_number_scope-]",
  anonymize: [# if options.anonymize #]true[# else #]false[# endif #],
  title-page: [# if options.title_page #]true[# else #]false[# endif #],
  abstract-page: [# if options.abstract_page #]true[# else #]false[# endif #],
[# if doc.bibtex #]
  bibliography-file: "[-doc.bibtex-]",
[# endif #]
)

[-IMPORTS-]

[-CONTENT-]
