package local

// Orient queries: high-signal aggregates an agent reads once at session
// start to bootstrap context on a Zotero library. Surfaced via
// `sci zot info --orient`.
//
// Each query is independent and cheap (immutable mode, no contention with
// the running desktop app). All exclude trashed items and child rows
// (attachments / notes) so counts reflect what users see in Zotero's UI.

import (
	"fmt"
	"time"
)

// HasMarkdownTag is the tag `sci zot extract` auto-applies to every
// parent item it generates a docling extraction for. Surfaced as the
// "can I read full PDF text on this paper" capability signal — agents
// reading the orient view know any item carrying this tag is queryable
// via `sci zot llm read|query` (or any markdown tooling).
const HasMarkdownTag = "has-markdown"

// TagCount is one tag with its item-count usage. Mirrors local.Tag minus
// the type field (orient view only surfaces user-meaningful tags, not
// the manual/automatic distinction).
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// CollectionRef is one collection's orient-view summary.
type CollectionRef struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RecentItem is one recently-added paper in compact form. Only the fields
// agents actually use to phrase a follow-up question — title (what is it),
// year (how recent), key (drill-in), date_added (recency signal).
type RecentItem struct {
	Key       string `json:"key"`
	Title     string `json:"title,omitempty"`
	Year      int    `json:"year,omitempty"`
	DateAdded string `json:"date_added,omitempty"`
}

// ExtractionCoverage reports how much of the library has full-text
// markdown extractions available to query. Source: count of items
// carrying the HasMarkdownTag (auto-applied by `sci zot extract`).
type ExtractionCoverage struct {
	WithExtraction int     `json:"with_extraction"`
	TotalItems     int     `json:"total_items"`
	Percent        float64 `json:"percent"`
}

// TopTags returns the n most-used tags in the library, descending by
// count, ties broken by name (case-insensitive). HasMarkdownTag is
// excluded — it's a capability signal already surfaced via
// ExtractionCoverage and would crowd out user-meaningful tags otherwise.
//
// n <= 0 returns no rows.
func (d *DB) TopTags(n int) ([]TagCount, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT tg.name, COUNT(DISTINCT it.itemID) AS c
		FROM tags tg
		JOIN itemTags it ON tg.tagID = it.tagID
		JOIN items i ON it.itemID = i.itemID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ?
		  AND di.itemID IS NULL
		  AND tg.name <> ?
		GROUP BY tg.tagID, tg.name
		ORDER BY c DESC, tg.name COLLATE NOCASE ASC
		LIMIT ?
	`, d.libraryID, HasMarkdownTag, n)
	if err != nil {
		return nil, fmt.Errorf("top tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []TagCount
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Name, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TopCollections returns the n collections with the most direct member
// items, descending. Direct membership only — items in nested children
// don't count toward the parent. (Agents wanting tree shape should call
// ListCollections.)
//
// n <= 0 returns no rows.
func (d *DB) TopCollections(n int) ([]CollectionRef, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT
			c.key,
			c.collectionName,
			(SELECT COUNT(*) FROM collectionItems ci WHERE ci.collectionID = c.collectionID) AS cnt
		FROM collections c
		WHERE c.libraryID = ?
		ORDER BY cnt DESC, c.collectionName COLLATE NOCASE ASC
		LIMIT ?
	`, d.libraryID, n)
	if err != nil {
		return nil, fmt.Errorf("top collections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []CollectionRef
	for rows.Next() {
		var c CollectionRef
		if err := rows.Scan(&c.Key, &c.Name, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecentlyAdded returns the n most-recently-added content items in the
// library, descending by dateAdded. Excludes attachments, notes, and
// trashed items.
//
// n <= 0 returns no rows.
func (d *DB) RecentlyAdded(n int) ([]RecentItem, error) {
	if n <= 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT
			i.key,
			i.dateAdded,
			(SELECT idv.value
				FROM itemData id
				JOIN itemDataValues idv ON id.valueID = idv.valueID
				JOIN fields f ON id.fieldID = f.fieldID
				WHERE id.itemID = i.itemID AND f.fieldName = 'title'
				LIMIT 1) AS title,
			(SELECT idv.value
				FROM itemData id
				JOIN itemDataValues idv ON id.valueID = idv.valueID
				JOIN fields f ON id.fieldID = f.fieldID
				WHERE id.itemID = i.itemID AND f.fieldName = 'date'
				LIMIT 1) AS date
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ?
		  AND di.itemID IS NULL
		  AND it.typeName NOT IN ('attachment','note')
		ORDER BY i.dateAdded DESC
		LIMIT ?
	`, d.libraryID, n)
	if err != nil {
		return nil, fmt.Errorf("recently added: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RecentItem
	for rows.Next() {
		var (
			r         RecentItem
			title, dt *string
			dateAdded string
		)
		if err := rows.Scan(&r.Key, &dateAdded, &title, &dt); err != nil {
			return nil, err
		}
		if title != nil {
			r.Title = *title
		}
		if dt != nil {
			r.Year = ParseYear(*dt)
		}
		// Zotero stores dateAdded in UTC as "YYYY-MM-DD HH:MM:SS"; emit
		// the date portion only — agents care about the day, not the
		// minute, and the wider format is noisy in JSON output.
		r.DateAdded = truncateZoteroTimestamp(dateAdded)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ExtractionCoverage reports the fraction of the library that has
// full-text markdown extractions available. Single SQL query — counts
// items carrying HasMarkdownTag and the total count separately.
//
// Percent is rounded to one decimal place; zero-item libraries return
// (0, 0, 0) without a divide-by-zero.
func (d *DB) ExtractionCoverage() (*ExtractionCoverage, error) {
	cov := &ExtractionCoverage{}
	if err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ?
		  AND di.itemID IS NULL
		  AND it.typeName NOT IN ('attachment','note')
	`, d.libraryID).Scan(&cov.TotalItems); err != nil {
		return nil, fmt.Errorf("count items: %w", err)
	}
	if err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT i.itemID)
		FROM items i
		JOIN itemTags ix ON i.itemID = ix.itemID
		JOIN tags tg ON ix.tagID = tg.tagID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ?
		  AND di.itemID IS NULL
		  AND tg.name = ?
	`, d.libraryID, HasMarkdownTag).Scan(&cov.WithExtraction); err != nil {
		return nil, fmt.Errorf("count has-markdown: %w", err)
	}
	if cov.TotalItems > 0 {
		// Round to one decimal: floor(x * 10 + 0.5) / 10.
		raw := float64(cov.WithExtraction) * 100.0 / float64(cov.TotalItems)
		cov.Percent = float64(int(raw*10+0.5)) / 10
	}
	return cov, nil
}

// truncateZoteroTimestamp keeps the date portion ("YYYY-MM-DD") of a
// Zotero timestamp string and drops the rest. Returns the input
// unchanged when it doesn't look like a Zotero timestamp.
func truncateZoteroTimestamp(s string) string {
	if len(s) < 10 {
		return s
	}
	// Validate the date portion is parseable so we don't truncate
	// arbitrary strings.
	if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
		return s
	}
	return s[:10]
}
