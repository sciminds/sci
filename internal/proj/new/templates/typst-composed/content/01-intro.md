# Introduction

This file is pure Markdown — no front matter, no Typst. Everything about the paper (title, authors, layout) lives in `typst.yml`; these files carry only prose. **Bold**, *italic*, `inline code`, footnotes[^1], and > blockquotes all work as written.

Citations come straight from `refs.bib` with no special syntax beyond Markdown's own: parenthetical [@jolly2019flatland], grouped when separated by a space [@jolly2019flatland] [@chang2021endogenous], with a locator [p. 5][@jolly2018pymer4], and narrative — @chang2021endogenous showed this style resolves to "Name (year)" in running text.
Cross-references use the same bracketed form: see [@fig-placeholder] and [@tbl-models] ahead, or link to a section by its heading text — [the analysis plan](#analysis) points at the Analysis heading with no id tags needed.

[^1]: Footnotes collect at the bottom of the page, numbered automatically.
