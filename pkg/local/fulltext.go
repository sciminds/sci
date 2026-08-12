package local

import (
	"fmt"
	"strings"
)

// SearchFulltext returns parent item IDs whose PDF attachments contain all of
// the given words in Zotero's fulltext word index.
//
// By default each word is prefix-matched (e.g. "neuro" matches "neuroimaging").
// When exact is true, words must match exactly. Words are lowercased before
// comparison — Zotero stores them lowercase in fulltextWords.
//
// Returns nil, nil when words is empty or the fulltext tables don't exist.
func (d *DB) SearchFulltext(words []string, exact bool) ([]int64, error) {
	if len(words) == 0 {
		return nil, nil
	}

	// Check that fulltext tables exist (non-Zotero DBs won't have them).
	if !d.hasFulltextTables() {
		return nil, nil
	}

	// Build one EXISTS clause per word for AND semantics.
	var clauses []string
	var args []any
	for _, w := range words {
		w = strings.ToLower(w)
		if exact {
			clauses = append(clauses, `EXISTS (
				SELECT 1 FROM fulltextItemWords fiw
				JOIN fulltextWords fw ON fw.wordID = fiw.wordID
				WHERE fiw.itemID = ia.itemID AND fw.word = ?)`)
		} else {
			clauses = append(clauses, `EXISTS (
				SELECT 1 FROM fulltextItemWords fiw
				JOIN fulltextWords fw ON fw.wordID = fiw.wordID
				WHERE fiw.itemID = ia.itemID AND fw.word LIKE ?)`)
			w += "%"
		}
		args = append(args, w)
	}

	q := `SELECT DISTINCT ia.parentItemID
FROM itemAttachments ia
WHERE ia.parentItemID IS NOT NULL
  AND ` + strings.Join(clauses, "\n  AND ")

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search fulltext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// hasFulltextTables checks whether the fulltextWords table exists. The result
// is cached for the lifetime of the DB handle.
func (d *DB) hasFulltextTables() bool {
	d.ftsOnce.Do(func() {
		var n int
		err := d.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='fulltextWords'`,
		).Scan(&n)
		d.hasFTS = err == nil && n > 0
	})
	return d.hasFTS
}
