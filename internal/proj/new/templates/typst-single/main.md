# Introduction

Write plain Markdown here — no front matter, no Typst. Everything about the paper (title, authors, layout) lives in `typst.yml`; this file carries only prose. Citations come straight from `refs.bib`: parenthetical [@jolly2019flatland], or narrative — @chang2021endogenous resolves to "Name (year)" in running text.

# Methods

Math is LaTeX, exactly as you already write it: inline $r = .42$, or display math between `$$` fences.

# Results

Figures are plain Markdown images: the alt text becomes the caption, and the *filename* becomes the reference label — an image at `figures/accuracy.svg` is citable as `[@fig-accuracy]`. Pipe tables with a `: caption {#tbl-id}` line beneath them become numbered, referenceable Tables.

# Discussion
