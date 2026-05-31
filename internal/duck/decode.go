// decode.go — parsing helpers for duckdb's -json output. Two concerns are
// split because Go's map loses key order: decodeRows reads values (with
// UseNumber for exact numeric text), and columnOrder recovers the projection
// order from the first object's key sequence.

package duck

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// decodeRows parses a duckdb -json array of objects, decoding numbers as
// json.Number so integer and decimal text survives verbatim (no float64
// rounding). Column order within each map is not preserved — use
// [columnOrder] for that.
func decodeRows(data []byte) ([]map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// columnOrder recovers the projection column order from the first object in a
// duckdb -json array. duckdb emits keys in SELECT order; Go's map decoding
// would otherwise scramble them. Returns nil for an empty result set.
func columnOrder(data []byte) ([]string, error) {
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
