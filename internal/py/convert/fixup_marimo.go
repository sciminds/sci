package convert

// fixup_marimo.go — post-conversion fixups for marimo notebooks: merges split
// import cells, cleans metadata, and normalizes cell structure.

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	// Match standalone import marimo cell followed by imports cell.
	reMergeMarimoImport = regexp.MustCompile(
		`@app\.cell\ndef _\(\):\n {4}import marimo as mo\n {4}return \(mo,\)\n\n\n@app\.cell\ndef _\(\):\n((?:` +
			`(?: {4}(?:#.*|from .+|import .+)| *)\n)+)` +
			` {4}return (.*)\n`,
	)

	// Cell tags comment lines.
	reCellTagComments = regexp.MustCompile(`(?m)^ {4}# Cell tags:.*\n`)

	// Match app.cell blocks for return tuple fixing.
	// The body pattern uses (?:    .*| *)\n to match both indented lines and blank lines.
	reCellPattern = regexp.MustCompile(
		`(?m)(@app\.cell(?:\(.*?\))?\ndef _\(.*?\):\n)((?:` +
			`(?: {4}.*| *)\n)*?)` +
			` {4}return (.*)\n`,
	)

	// Import patterns.
	reImportAs     = regexp.MustCompile(`^import\s+\S+\s+as\s+(\w+)`)
	reImportSimple = regexp.MustCompile(`^import\s+(\w+)$`)
	reFromImport   = regexp.MustCompile(`^from\s+\S+\s+import\s+(.+)`)

	// Simple assignment.
	reAssignment = regexp.MustCompile(`^(\w+)\s*=\s`)

	// MyST admonitions: generic with title and :class:
	reAdmonitionGenericMarimo = regexp.MustCompile(`(?m)^([ ]*):::+\{admonition\}\s*(.*)\n[ ]*:class:\s*\w+\n((?:.*\n)*?)[ ]*:::+`)

	// MyST admonitions: native shorthand.
	reAdmonitionShorthandMarimo = regexp.MustCompile(`(?m)^([ ]*):::+\{(\w+)\}\n((?:.*\n)*?)[ ]*:::+`)
)

func mergeMarimoImportCell(text string) string {
	return reMergeMarimoImport.ReplaceAllStringFunc(text, func(match string) string {
		sub := reMergeMarimoImport.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		importLines := sub[1]

		// Strip cell tag comments
		cleaned := reCellTagComments.ReplaceAllString(importLines, "")

		names := extractImportNames(cleaned)
		names["mo"] = true
		sorted := slices.Sorted(maps.Keys(names))
		returnStr := strings.Join(sorted, ", ")

		return "@app.cell\ndef _():\n    import marimo as mo\n" + cleaned + "    return " + returnStr + "\n"
	})
}

func stripCellTagComments(text string) string {
	return reCellTagComments.ReplaceAllString(text, "")
}

func fixReturnTuples(text string) string {
	return reCellPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := reCellPattern.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		header := sub[1]
		body := sub[2]
		oldReturn := sub[3]

		// Skip markdown cells
		if strings.Contains(body, "mo.md(") {
			return match
		}
		if strings.TrimSpace(oldReturn) == "" {
			return match
		}

		names := make(map[string]bool)
		for _, line := range strings.Split(body, "\n") {
			stripped := strings.TrimSpace(line)

			// from X import a, b
			if m := reFromImport.FindStringSubmatch(stripped); m != nil {
				for _, name := range strings.Split(m[1], ",") {
					name = strings.TrimSpace(name)
					if strings.Contains(name, " as ") {
						parts := strings.Split(name, " as ")
						name = strings.TrimSpace(parts[len(parts)-1])
					}
					if name != "" && isIdentifier(name) {
						names[name] = true
					}
				}
				continue
			}

			// import X as Y
			if m := reImportAs.FindStringSubmatch(stripped); m != nil {
				names[m[1]] = true
				continue
			}

			// import X
			if m := reImportSimple.FindStringSubmatch(stripped); m != nil {
				names[m[1]] = true
				continue
			}

			// Simple assignments: x = ...
			if m := reAssignment.FindStringSubmatch(stripped); m != nil {
				if m[1] != "_" {
					names[m[1]] = true
				}
			}
		}

		if len(names) == 0 {
			return match
		}

		// Parse existing return tuple
		existing := make(map[string]bool)
		retStr := strings.TrimSpace(oldReturn)
		retStr = strings.TrimPrefix(retStr, "(")
		retStr = strings.TrimSuffix(retStr, ")")
		for _, name := range strings.Split(retStr, ",") {
			n := strings.TrimSpace(name)
			if n != "" {
				existing[n] = true
			}
		}

		// Merge
		merged := make(map[string]bool)
		for k := range existing {
			merged[k] = true
		}
		for k := range names {
			merged[k] = true
		}
		if len(merged) == len(existing) {
			return match
		}

		sorted := slices.Sorted(maps.Keys(merged))
		var returnLine string
		if len(sorted) == 1 {
			returnLine = "(" + sorted[0] + ",)"
		} else {
			returnLine = strings.Join(sorted, ", ")
		}

		return header + body + "    return " + returnLine + "\n"
	})
}

func convertAdmonitionsToBlockquotes(text string) string {
	// Generic form: :::{admonition} Title\n:class: kind\n...\n:::
	text = reAdmonitionGenericMarimo.ReplaceAllStringFunc(text, func(match string) string {
		sub := reAdmonitionGenericMarimo.FindStringSubmatch(match)
		indent := sub[1]
		title := strings.TrimSpace(sub[2])
		body := sub[3]
		return admonitionToBlockquote(indent, title, body)
	})
	// Native shorthand: :::{note}\n...\n:::
	text = reAdmonitionShorthandMarimo.ReplaceAllStringFunc(text, func(match string) string {
		sub := reAdmonitionShorthandMarimo.FindStringSubmatch(match)
		indent := sub[1]
		kind := sub[2]
		body := sub[3]
		if calloutTypes[kind] {
			return admonitionToBlockquote(indent, capitalize(kind), body)
		}
		return match
	})
	return text
}

func admonitionToBlockquote(indent, title, body string) string {
	lines := []string{
		indent + "> **" + title + "**",
		indent + ">",
	}
	for _, line := range strings.Split(body, "\n") {
		stripped := strings.TrimRight(line, " \t")
		if stripped != "" {
			lines = append(lines, indent+"> "+strings.TrimLeft(stripped, " \t"))
		} else {
			lines = append(lines, indent+">")
		}
	}
	// Remove trailing empty blockquote lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == ">" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// FixupsAfterToMarimo applies all fixups for → marimo conversion.
func FixupsAfterToMarimo(text string) string {
	text = mergeMarimoImportCell(text)
	text = stripCellTagComments(text)
	text = fixReturnTuples(text)
	text = convertAdmonitionsToBlockquotes(text)
	text = CollapseBlankLines(text)
	return text
}

// extractImportNames extracts all names introduced by import statements.
func extractImportNames(code string) map[string]bool {
	names := make(map[string]bool)
	for _, line := range strings.Split(code, "\n") {
		stripped := strings.TrimSpace(line)

		if m := reImportAs.FindStringSubmatch(stripped); m != nil {
			names[m[1]] = true
			continue
		}
		if m := reImportSimple.FindStringSubmatch(stripped); m != nil {
			names[m[1]] = true
			continue
		}
		if m := reFromImport.FindStringSubmatch(stripped); m != nil {
			for _, name := range strings.Split(m[1], ",") {
				name = strings.TrimSpace(name)
				if strings.Contains(name, " as ") {
					parts := strings.Split(name, " as ")
					name = strings.TrimSpace(parts[len(parts)-1])
				}
				if name != "" && isIdentifier(name) {
					names[name] = true
				}
			}
		}
	}
	return names
}

var reIdentifier = regexp.MustCompile(`^\w+$`)

func isIdentifier(s string) bool {
	return len(s) > 0 && reIdentifier.MatchString(s)
}
