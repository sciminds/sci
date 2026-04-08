import marimo

__generated_with = "0.20.2"
app = marimo.App()


@app.cell
def _(mo):
    mo.md(r"""
    # Demo Analysis Notebook

    This notebook demonstrates a common analysis workflow:

    1. Load data from `data/raw/`
    2. Clean and transform it
    3. Generate a figure and save it to `figs/`

    The saved figure is then referenced in the report which renders to PDF.
    """)
    return


@app.cell
def _():
    import polars as pl
    import seaborn as sns
    from pathlib import Path

    DATADIR = Path("../../data/raw")
    FIGDIR = Path("../../figs")

    penguins = pl.read_csv(DATADIR / "penguins.csv")
    penguins.head()
    return FIGDIR, penguins, sns


@app.cell
def _(penguins, sns):
    fig = (
        sns.catplot(
            penguins.to_pandas(),
            x="species",
            y="flipper_length_mm",
            kind="violin",
            hue="species",
            legend=False,
        )
        .set_ylabels("Flipper Length (mm)")
        .set_xlabels("")
        .set(title="Palmer Penguin Species Differences")
    )
    fig
    return (fig,)


@app.cell
def _(FIGDIR, fig):
    fig.savefig(FIGDIR / "penguin_flipper_lengths.png", bbox_inches="tight")
    return


if __name__ == "__main__":
    app.run()
