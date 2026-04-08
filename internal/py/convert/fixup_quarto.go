package convert

// fixup_quarto.go — post-conversion fixups for Quarto (.qmd): strips jupytext
// kernelspec metadata and marimo cell-option comments.

import (
	"regexp"
	"strings"
)

var (
	// Strip jupytext/kernelspec metadata from Quarto YAML frontmatter.
	reJupytextFrontmatterQmd = regexp.MustCompile(`(?s)^(---\n(?:.*\n)*?)(jupyter:\n(?:[ ].*\n)*)(.*?---\n)`)

	// Strip #| marimo: {...} cell options from Quarto code cells.
	reMarimoCellMetadataQmd = regexp.MustCompile(`(?m)^#\| marimo:.*\n`)

	// Match first python code cell for tagging.
	reFirstPythonCell = regexp.MustCompile("(?s)```\\{python\\}\n(.*?)```")

	// Bare --- horizontal rules (outside frontmatter).
	reBareHR = regexp.MustCompile(`(?m)^---$`)

	// MyST admonitions: generic with title and :class:
	reAdmonitionGeneric = regexp.MustCompile(`(?m):::\{admonition\}\s+(.*)\n:class:\s*(\w+)`)

	// MyST admonitions: native shorthand.
	reAdmonitionShorthand = regexp.MustCompile(`:::\{(\w+)\}`)
)

var calloutTypes = map[string]bool{
	"note": true, "tip": true, "warning": true, "important": true, "caution": true,
}

func stripJupytextFrontmatterQmd(text string) string {
	return reJupytextFrontmatterQmd.ReplaceAllString(text, "${1}${3}")
}

func stripMarimoCellMetadataQmd(text string) string {
	return reMarimoCellMetadataQmd.ReplaceAllString(text, "")
}

func tagFirstCellQuarto(text string) string {
	replaced := false
	return reFirstPythonCell.ReplaceAllStringFunc(text, func(match string) string {
		if replaced {
			return match
		}
		replaced = true
		sub := reFirstPythonCell.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		code := sub[1]
		return "```{python}\n#| include: false\n" + code + "```"
	})
}

func convertAdmonitions(text string) string {
	// Generic admonition with title and :class: → titled callout
	text = reAdmonitionGeneric.ReplaceAllStringFunc(text, func(match string) string {
		sub := reAdmonitionGeneric.FindStringSubmatch(match)
		title := strings.TrimSpace(sub[1])
		kind := sub[2]
		if !calloutTypes[kind] {
			kind = "note"
		}
		return `:::{.callout-` + kind + ` title="` + title + `"}`
	})
	// Native MyST shorthand → titleless callout
	text = reAdmonitionShorthand.ReplaceAllStringFunc(text, func(match string) string {
		sub := reAdmonitionShorthand.FindStringSubmatch(match)
		kind := sub[1]
		if calloutTypes[kind] {
			return ":::{.callout-" + kind + "}"
		}
		return match
	})
	return text
}

func safeHorizontalRules(text string) string {
	// Find end of YAML frontmatter (if present)
	start := 0
	if strings.HasPrefix(text, "---\n") {
		endIdx := strings.Index(text[4:], "\n---\n")
		if endIdx != -1 {
			start = endIdx + 4 + 5 // skip past closing ---\n
		}
	}
	frontmatter := text[:start]
	body := text[start:]
	replaced := reBareHR.ReplaceAllString(body, "***")
	return frontmatter + replaced
}

// FixupsAfterToQuarto applies all fixups for → Quarto conversion.
func FixupsAfterToQuarto(text string, fromMarimo bool) string {
	if fromMarimo {
		text = stripFrontmatterBody(text)
		text = stripMarimoMetadata(text)
		text = stripJupytextFrontmatterQmd(text)
		text = stripMarimoCellMetadataQmd(text)
		text = stripMarimoImport(text)
		text = tagFirstCellQuarto(text)
	}
	text = convertAdmonitions(text)
	text = safeHorizontalRules(text)
	text = CollapseBlankLines(text)
	return text
}
