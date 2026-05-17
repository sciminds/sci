package duck

// mirror.go — materialise a .duckdb file as a SQLite database. Used by
// `sci view foo.duckdb` to open duckdb files in dbtui, which is SQLite-
// only. The mirror is intended to be a tempfile opened read-only; the
// caller deletes it on exit.
//
// Type fidelity caveat: duckdb's STRUCT, LIST, MAP, and INTERVAL types
// are coerced to TEXT by the sqlite_scanner's CREATE TABLE AS path.
// Numeric and string columns round-trip cleanly. Complex-typed columns
// will appear stringified to the user — acceptable for v1 of the
// duckdb-backed viewer.

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
)

// BuildSQLiteMirror copies every table and view from src (.duckdb) into
// dest (.db) via a single duckdb invocation. dest must not yet exist.
// Returns ErrNotInstalled (wrapped) when the duckdb binary is missing.
func BuildSQLiteMirror(src, dest string) error {
	metas, err := Info(src)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", src, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "ATTACH '%s' AS d (READ_ONLY);", sqlEscape(src))
	fmt.Fprintf(&b, " ATTACH '%s' AS m (TYPE SQLITE);", sqlEscape(dest))
	for _, m := range metas {
		// Table names are validated by Info() against IsSafeIdentifier
		// before they reach us, so direct interpolation is safe.
		fmt.Fprintf(&b, ` CREATE TABLE m."%s" AS SELECT * FROM d."%s";`, m.Name, m.Name)
	}
	b.WriteString(" DETACH d; DETACH m;")

	if _, err := runJSON(b.String()); err != nil {
		return fmt.Errorf("build mirror %s → %s: %w", src, dest, err)
	}
	return nil
}

// MirrorTables returns the names of tables that BuildSQLiteMirror would
// materialise. Provided so callers can decide whether to surface a
// "(no tables)" hint before launching the viewer.
func MirrorTables(src string) ([]string, error) {
	metas, err := Info(src)
	if err != nil {
		return nil, err
	}
	return lo.Map(metas, func(m TableMeta, _ int) string { return m.Name }), nil
}
