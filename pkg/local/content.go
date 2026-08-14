package local

import (
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// itemIDBatch caps how many keys go into one IN clause. SQLite's
// parameter limit is far higher, but batching keeps the statement small
// enough to plan quickly when a broad content query matches thousands of
// items.
const itemIDBatch = 500

// ItemIDsForKeys resolves item keys to local row ids, skipping keys that
// are unknown or trashed. A [SearchOptions.Content] hook reports matches
// by item key (stable, and what the Zotero API speaks) while the search
// clauses filter on row ids, so this is the bridge between them.
func (d *DB) ItemIDsForKeys(keys []string) ([]int64, error) {
	var out []int64
	for chunk := range slices.Chunk(keys, itemIDBatch) {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		q := `
SELECT i.itemID
FROM items i
LEFT JOIN deletedItems d ON d.itemID = i.itemID
WHERE i.libraryID = ? AND d.itemID IS NULL AND i.key IN (` + placeholders + `)`

		args := slices.Concat([]any{d.libraryID}, lo.ToAnySlice(chunk))
		rows, err := d.db.Query(q, args...)
		if err != nil {
			return nil, fmt.Errorf("resolve item keys: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}
