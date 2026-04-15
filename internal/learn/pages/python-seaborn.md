# Seaborn (Data Viz)

## Setup

```python
import seaborn as sns
import polars as pl

# Seaborn needs pandas DataFrames
penguins = pl.DataFrame(sns.load_dataset("penguins"))
data = penguins.to_pandas()
```

## Core Idea

Map data columns to visual properties:

- `x`, `y` — axis positions
- `hue` — color by category
- `col`, `row` — subplot panels
- `style` — marker/line styles
- `size` — point sizes

## Relationships: relplot()

```python
sns.relplot(data=data, x="flipper_length_mm", y="body_mass_g")

# Color by species
sns.relplot(data=data, x="flipper_length_mm", y="body_mass_g",
            hue="species")

# Facet into panels
sns.relplot(data=data, x="flipper_length_mm", y="body_mass_g",
            hue="species", col="species", height=3)
```

## Regression: lmplot()

```python
sns.lmplot(data=data, x="flipper_length_mm", y="body_mass_g",
           col="species")
```

## Distributions: displot()

```python
# Histogram
sns.displot(data=data, x="body_mass_g", kind="hist", bins=30)

# By group (stacked)
sns.displot(data=data, x="body_mass_g", kind="hist",
            hue="species", multiple="stack")

# KDE (smooth density)
sns.displot(data=data, x="body_mass_g", kind="kde",
            hue="species", fill=True)

# 2D density
sns.displot(data=data, x="flipper_length_mm", y="body_mass_g",
            kind="kde", hue="species")
```

## Categories: catplot()

```python
# Individual observations
sns.catplot(data=data, x="species", y="body_mass_g", kind="strip")
sns.catplot(data=data, x="species", y="body_mass_g", kind="swarm")

# Distributions
sns.catplot(data=data, x="species", y="body_mass_g",
            hue="sex", kind="box")
sns.catplot(data=data, x="species", y="body_mass_g",
            hue="sex", kind="violin")

# Summaries (mean + 95% CI)
sns.catplot(data=data, x="species", y="body_mass_g",
            hue="sex", kind="bar")
sns.catplot(data=data, x="species", y="body_mass_g",
            hue="sex", kind="point")
```

## Multi-Panel Views

```python
# Scatter + marginal distributions
sns.jointplot(data=data, x="flipper_length_mm", y="body_mass_g",
              hue="species")

# All pairwise relationships
sns.pairplot(data=data, hue="species")
```

## Layering Plots

```python
grid = sns.catplot(data=data, x="species", y="body_mass_g",
                   hue="sex", kind="bar")

grid.map_dataframe(sns.stripplot, x="species", y="body_mass_g",
                   hue="sex", dodge=True, alpha=0.5)
```

## Themes & Context

```python
sns.set_theme(style="whitegrid", context="talk")
```

**Styles:** darkgrid, whitegrid, dark, white, ticks
**Contexts:** paper, notebook (default), talk, poster

## Color Palettes

```python
sns.catplot(..., palette="Set2")
```

**Categorical:** Set1, Set2, Paired, tab10
**Sequential:** Blues, Greens, viridis, rocket
**Diverging:** coolwarm, RdBu, vlag

## Customization

```python
g = sns.relplot(data=data, x="flipper_length_mm", y="body_mass_g",
                col="species", height=3)

g.set_axis_labels("Flipper (mm)", "Mass (g)")
g.set_titles("{col_name}")
g.set(xlim=(100, 250), ylim=(2000, 6500))
g.figure.suptitle("Penguins", y=1.02)
g.tight_layout()
```

## Controlling Order

```python
sns.catplot(..., order=["Chinstrap", "Adelie", "Gentoo"])
# Also: hue_order, col_order, row_order
```

## Finishing Touches

```python
# Move legend
sns.move_legend(g, "upper left", bbox_to_anchor=(1, 1))

# Remove legend
sns.relplot(..., legend=False)

# Rotate tick labels
g.set_xticklabels(rotation=45, ha="right")

# Remove spines
sns.despine()
```

## Save Figures

```python
g.savefig("figure.png", dpi=300, bbox_inches="tight")
g.savefig("figure.pdf", bbox_inches="tight")  # vector

# Publication preset
sns.set_theme(style="ticks", context="paper",
              rc={"savefig.dpi": 300})
```

## Quick Reference

| Task | Function |
|------|----------|
| Numeric relationships | `relplot(kind="scatter"\|"line")` |
| Distributions | `displot(kind="hist"\|"kde"\|"ecdf")` |
| Categories | `catplot(kind="strip"\|"box"\|"violin"\|"bar"\|"point")` |
| Regression lines | `lmplot()` |
| Joint view | `jointplot()` |
| All pairs | `pairplot()` |
