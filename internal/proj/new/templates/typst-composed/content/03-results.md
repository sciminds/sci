# Results

Figures are plain Markdown images. The alt text becomes the caption (markdown and math work inside it), and the *filename* becomes the reference label: `figures/placeholder.svg` is citable as `[@fig-placeholder]`. Attributes size a figure while it stays centered — this one is `{width=60%}` — and `{#fig-custom}` overrides the label when you need to (here, because [@fig-custom] and a panel below share the same image file):

![A single-panel figure at 60% width. Drop in any SVG/PNG exported from Python — vector SVG stays crisp.](figures/placeholder.svg){width=60% #fig-custom}

Panel figures use Quarto's div syntax: 2–4 images become one numbered figure — side-by-side (`layout-ncol=2`), stacked (`layout-ncol=1`), or a quadrant (4 images, `layout-ncol=2`). The whole is [@fig-panels]; panels reference individually as [@fig-placeholder-b] and render as 1a, 1b:

::: {#fig-panels layout-ncol=2}
![Behavioral accuracy.](figures/placeholder.svg)
![Model fits.](figures/placeholder-b.svg)

Two panels side by side, each with its own subcaption.
:::

Tables are pipe tables. Add a caption line starting with `:` directly beneath one — with an optional `{#tbl-id}` — and it becomes a numbered, referenceable Table. With `align_decimals: true`, numeric columns line up on the decimal point while rendering exactly as you typed them (`.31` stays `.31`):

| Model  |     AIC |     BIC | $R^2$ |
| :----- | ------: | ------: | ----: |
| Null   |  1204.1 | 1210.33 |     — |
| Linear | 1150.75 |  1163.2 |   .31 |
| Full   |   987.4 |    1121 |  .448 |

: Model comparison. Captions take markdown and math too, e.g. $R^2$. {#tbl-models}

A table without a caption line stays a plain, unnumbered table.

## Discussion

What these files cannot do: execute code (figures are generated in Python and committed), and Typst-only tricks require a `<!--raw-typst ... -->` escape comment. If you find yourself needing many of those, you want the kit's `.typ` path instead.
