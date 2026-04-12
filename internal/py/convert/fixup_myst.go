package convert

// fixup_myst.go — post-conversion fixups for MyST markdown: strips spurious
// frontmatter, cell metadata, and jupytext artifacts.

import (
	"regexp"
	"strings"

	"github.com/samber/lo"
)

var (
	// Remove the spurious frontmatter YAML block that marimo notebooks emit.
	reFrontmatterBody = regexp.MustCompile(`(?s)\+\+\+\s*\{.*?\}\s*\n\s*---\n(?:title:.*\n|author:.*\n|date:.*\n)+---\n`)

	// Remove +++ {"marimo": ...} cell metadata lines.
	reMarimoMetadata = regexp.MustCompile(`(?m)^\+\+\+\s*\{.*\}\s*$`)

	// Strip the jupytext metadata YAML block from MyST frontmatter.
	reJupytextFrontmatterMyst = regexp.MustCompile(`(?m)^---\njupytext:\n(?:[ ].*\n)*---\n\n?`)

	// Strip marimo cell metadata blocks inside MyST code cells.
	reMarimoCellMetadataMyst = regexp.MustCompile("(```\\{code-cell\\}\\s*\\w+\n)---\nmarimo:\n(?:[ ].*\n)*---\n")

	// Match first code cell for tagging.
	reFirstCodeCell = regexp.MustCompile("(?s)```\\{code-cell\\}\\s*(\\w+)\n(.*?)```")

	// Remove import marimo as mo lines.
	reMarimoImport = regexp.MustCompile(`(?m)^import marimo as mo\n?`)

	// Normalize ipython3 → python.
	reIPython3 = regexp.MustCompile(`\{code-cell\} ipython3`)

	// Quarto callouts with title → MyST admonitions.
	reCalloutTitled = regexp.MustCompile(`:::\{\.callout-(\w+)\s+title="((?:[^"\\]|\\.)*)"\}`)

	// Quarto callouts without title → MyST shorthand.
	reCalloutTitleless = regexp.MustCompile(`:::\{\.callout-(\w+)\}`)
)

func stripFrontmatterBody(text string) string {
	return reFrontmatterBody.ReplaceAllString(text, "")
}

func stripMarimoMetadata(text string) string {
	return reMarimoMetadata.ReplaceAllString(text, "")
}

func stripJupytextFrontmatterMyst(text string) string {
	return reJupytextFrontmatterMyst.ReplaceAllString(text, "")
}

func stripMarimoCellMetadataMyst(text string) string {
	return reMarimoCellMetadataMyst.ReplaceAllString(text, "$1")
}

func tagImportsCell(text string) string {
	replaced := false
	return reFirstCodeCell.ReplaceAllStringFunc(text, func(match string) string {
		if replaced {
			return match
		}
		replaced = true
		sub := reFirstCodeCell.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		lang := sub[1]
		code := sub[2]
		// Remove import marimo as mo lines from code
		lines := strings.Split(code, "\n")
		filtered := lo.Reject(lines, func(ln string, _ int) bool {
			return strings.TrimSpace(ln) == "import marimo as mo"
		})
		return "```{code-cell} " + lang + "\n:tags: [remove-cell]\n" + strings.Join(filtered, "\n") + "```"
	})
}

func stripMarimoImport(text string) string {
	return reMarimoImport.ReplaceAllString(text, "")
}

func normalizeCodeLang(text string) string {
	return reIPython3.ReplaceAllString(text, "{code-cell} python")
}

func convertCallouts(text string) string {
	// Titled callouts → generic admonition with :class:
	text = reCalloutTitled.ReplaceAllStringFunc(text, func(match string) string {
		sub := reCalloutTitled.FindStringSubmatch(match)
		kind := sub[1]
		title := sub[2]
		return ":::{admonition} " + title + "\n:class: " + kind
	})
	// Titleless callouts → native MyST shorthand
	text = reCalloutTitleless.ReplaceAllStringFunc(text, func(match string) string {
		sub := reCalloutTitleless.FindStringSubmatch(match)
		kind := sub[1]
		return ":::{" + kind + "}"
	})
	return text
}

// FixupsAfterToMyst applies all fixups for → MyST conversion.
func FixupsAfterToMyst(text string, fromMarimo bool) string {
	if fromMarimo {
		text = stripFrontmatterBody(text)
		text = stripMarimoMetadata(text)
		text = stripJupytextFrontmatterMyst(text)
		text = stripMarimoCellMetadataMyst(text)
		text = tagImportsCell(text)
		text = stripMarimoImport(text)
	}
	text = normalizeCodeLang(text)
	text = convertCallouts(text)
	text = CollapseBlankLines(text)
	return text
}
