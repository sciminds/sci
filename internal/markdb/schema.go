package markdb

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ColumnDef describes a dynamically discovered frontmatter column.
type ColumnDef struct {
	Key          string // original YAML key
	ColumnName   string // sanitized SQL column name
	InferredType string // text, integer, real, json
	FileCount    int    // how many files have this key
	Sample       string // first non-nil value as string
}

// reservedColumns are structural column names in the files table.
var reservedColumns = map[string]bool{
	"id": true, "path": true, "source_id": true,
	"frontmatter_raw": true, "body": true, "body_text": true,
	"frontmatter_text": true, "mtime": true, "hash": true,
	"parse_error": true,
}

var reNonAlnum = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// SanitizeColumnName converts a YAML key to a safe SQL column name.
func SanitizeColumnName(key string) string {
	col := reNonAlnum.ReplaceAllString(key, "_")

	// Leading digit.
	if len(col) > 0 && unicode.IsDigit(rune(col[0])) {
		col = "_" + col
	}

	// Reserved name collision.
	if reservedColumns[strings.ToLower(col)] {
		col = "fm_" + col
	}

	return col
}

// InferType determines the SQL type from a set of observed values.
// Widening ladder: bool < integer < real < text. Lists/maps → json.
func InferType(values []any) string {
	if len(values) == 0 {
		return "text"
	}

	hasJSON := false
	best := "" // "", "bool", "integer", "real", "text"

	for _, v := range values {
		if v == nil {
			continue
		}

		var vType string
		switch v.(type) {
		case bool:
			vType = "bool"
		case int, int64:
			vType = "integer"
		case float64:
			vType = "real"
		case string:
			vType = "text"
		case []any:
			hasJSON = true
		case map[string]any:
			hasJSON = true
		default:
			vType = "text"
		}

		if hasJSON {
			return "json"
		}

		best = widenType(best, vType)
	}

	if best == "" || best == "bool" {
		if best == "bool" {
			return "integer"
		}
		return "text"
	}
	return best
}

// widenType returns the wider of two types.
func widenType(a, b string) string {
	rank := map[string]int{"": 0, "bool": 1, "integer": 2, "real": 3, "text": 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// DiscoverSchema collects all frontmatter keys across parsed files
// and infers column definitions.
func DiscoverSchema(parsed []map[string]any) []ColumnDef {
	keyValues := make(map[string][]any)
	keyCounts := make(map[string]int)
	keySamples := make(map[string]string)

	for _, m := range parsed {
		if m == nil {
			continue
		}
		for k, v := range m {
			keyValues[k] = append(keyValues[k], v)
			keyCounts[k]++
			if _, exists := keySamples[k]; !exists && v != nil {
				keySamples[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(keyValues))
	for k := range keyValues {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cols := make([]ColumnDef, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, ColumnDef{
			Key:          k,
			ColumnName:   SanitizeColumnName(k),
			InferredType: InferType(keyValues[k]),
			FileCount:    keyCounts[k],
			Sample:       keySamples[k],
		})
	}
	return cols
}
