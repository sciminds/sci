// decode.go — parsing helpers for duckdb's -json output. Two concerns are
// split because Go's map loses key order: decodeRows reads values (with
// UseNumber for exact numeric text), and columnOrder recovers the projection
// order from the first object's key sequence.

package duck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// specialFloatReplacements maps DuckDB's bare (non-JSON) float literals to the
// quoted strings we substitute for them. Order matters: "-Infinity" must be
// tried before "Infinity" so the leading minus is consumed as one token.
var specialFloatReplacements = []struct{ token, repl []byte }{
	{[]byte("-Infinity"), []byte(`"-Inf"`)},
	{[]byte("Infinity"), []byte(`"Inf"`)},
	{[]byte("NaN"), []byte(`"NaN"`)},
}

// SanitizeSpecialFloats rewrites the bare NaN / Infinity / -Infinity tokens that
// `duckdb -json`/`-jsonlines` emit for special double values into quoted strings
// ("NaN"/"Inf"/"-Inf"), so encoding/json — which rejects those bare tokens with
// "invalid character 'N'/'I'" — can decode the row. Tokens inside JSON string
// values are left untouched; input with no such tokens is returned unchanged
// (and is never mutated in place).
func SanitizeSpecialFloats(raw []byte) []byte {
	out := raw
	inString := false
	escaped := false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		for _, sf := range specialFloatReplacements {
			if i+len(sf.token) <= len(out) && bytes.Equal(out[i:i+len(sf.token)], sf.token) {
				// slices.Concat copies, so raw is never mutated.
				out = slices.Concat(out[:i], sf.repl, out[i+len(sf.token):])
				i += len(sf.repl) - 1
				break
			}
		}
	}
	return out
}

// decodeRows parses a duckdb -json array of objects, decoding numbers as
// json.Number so integer and decimal text survives verbatim (no float64
// rounding). Column order within each map is not preserved — use
// [columnOrder] for that. Special-float tokens are sanitized first so the
// standard JSON decoder accepts them.
func decodeRows(data []byte) ([]map[string]any, error) {
	data = SanitizeSpecialFloats(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// unmarshalJSON decodes a duckdb -json result into v, sanitizing the bare
// NaN/Infinity/-Infinity tokens first (see [SanitizeSpecialFloats]) so the
// standard decoder accepts them. It is the struct-decode counterpart to
// [decodeRows]/[columnOrder]: every path that decodes duckdb -json output runs
// through the same dialect-aware sanitize step, so a special float in any
// numeric cell can't reach json.Unmarshal as an invalid token.
func unmarshalJSON(data []byte, v any) error {
	return json.Unmarshal(SanitizeSpecialFloats(data), v)
}

// columnOrder recovers the projection column order from the first object in a
// duckdb -json array. duckdb emits keys in SELECT order; Go's map decoding
// would otherwise scramble them. Returns nil for an empty result set. Special-
// float tokens are sanitized first so the value-skipping tokenizer accepts them.
func columnOrder(data []byte) ([]string, error) {
	data = SanitizeSpecialFloats(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	// Opening '[' of the array.
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if !dec.More() {
		return nil, nil // empty array
	}
	// Opening '{' of the first object.
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var cols []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %T", keyTok)
		}
		cols = append(cols, key)
		if err := skipValue(dec); err != nil {
			return nil, err
		}
	}
	return cols, nil
}

// skipValue consumes the next complete JSON value from dec — scalar or a
// fully-nested object/array — so columnOrder can advance to the next key
// without being confused by nested STRUCT/LIST columns.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil // scalar value, fully consumed
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
		}
	}
	return nil
}
