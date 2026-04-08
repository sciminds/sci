package convert

import (
	"strings"
	"testing"
)

func TestExtractImportNames(t *testing.T) {
	code := `    import numpy as np
    import os
    from pathlib import Path
    from collections import defaultdict, OrderedDict
    from typing import List as Lst`
	names := extractImportNames(code)

	for _, want := range []string{"np", "os", "Path", "defaultdict", "OrderedDict", "Lst"} {
		if !names[want] {
			t.Errorf("expected %q in extracted names", want)
		}
	}
}

func TestMergeMarimoImportCell(t *testing.T) {
	input := `@app.cell
def _():
    import marimo as mo
    return (mo,)


@app.cell
def _():
    import numpy as np
    from pathlib import Path
    return np, Path
`
	got := mergeMarimoImportCell(input)
	if strings.Contains(got, "return (mo,)") {
		t.Error("standalone mo cell should be merged")
	}
	if !strings.Contains(got, "import marimo as mo") {
		t.Error("marimo import should be preserved in merged cell")
	}
	if !strings.Contains(got, "import numpy as np") {
		t.Error("numpy import should be in merged cell")
	}
	// Return should include mo, np, Path
	if !strings.Contains(got, "mo") || !strings.Contains(got, "np") || !strings.Contains(got, "Path") {
		t.Errorf("return should include all names, got: %s", got)
	}
}

func TestStripCellTagComments(t *testing.T) {
	input := "    # Cell tags: remove-cell\n    import numpy as np\n"
	got := stripCellTagComments(input)
	if strings.Contains(got, "Cell tags") {
		t.Error("cell tag comments should be stripped")
	}
	if !strings.Contains(got, "import numpy") {
		t.Error("code should be preserved")
	}
}

func TestFixReturnTuples(t *testing.T) {
	input := `@app.cell
def _():
    import numpy as np
    x = 42
    return (np,)
`
	got := fixReturnTuples(input)
	if !strings.Contains(got, "np") {
		t.Error("np should be in return")
	}
	if !strings.Contains(got, "x") {
		t.Error("x should be added to return")
	}
}

func TestFixReturnTuplesWithBlankLines(t *testing.T) {
	input := `@app.cell
def _():
    import numpy as np

    x = np.array([1, 2, 3])
    return (np,)
`
	got := fixReturnTuples(input)
	if !strings.Contains(got, "x") {
		t.Error("x should be added to return even with blank lines in cell body")
	}
	if !strings.Contains(got, "np") {
		t.Error("np should still be in return")
	}
}

func TestFixReturnTuplesSkipsMd(t *testing.T) {
	input := `@app.cell
def _(mo):
    mo.md(r"""# Title""")
    return
`
	got := fixReturnTuples(input)
	// Should not modify markdown cells (empty return)
	if got != input {
		t.Errorf("markdown cells should not be modified, got:\n%s", got)
	}
}

func TestConvertAdmonitionsToBlockquotes(t *testing.T) {
	input := ":::{note}\nThis is important.\n:::"
	got := convertAdmonitionsToBlockquotes(input)
	if !strings.Contains(got, "> **Note**") {
		t.Error("admonition should be converted to blockquote")
	}
	if !strings.Contains(got, "> This is important.") {
		t.Error("body should be in blockquote")
	}
}

func TestCollapseBlankLines(t *testing.T) {
	input := "a\n\n\n\nb"
	got := CollapseBlankLines(input)
	if got != "a\n\nb" {
		t.Errorf("expected collapsed blank lines, got: %q", got)
	}
}
