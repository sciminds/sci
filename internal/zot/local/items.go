package local

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/match"
)

// fieldValueSubquery is a reusable correlated subquery that pulls a single
// EAV field value for the current items row. Kept as a raw string rather
// than a prepared helper because it appears multiple times in the same
// SELECT and sqlite is perfectly happy to reuse plans.
const fieldValueSubquery = `
	(SELECT idv.value
	 FROM itemData id
	 JOIN fields f ON id.fieldID = f.fieldID
	 JOIN itemDataValues idv ON id.valueID = idv.valueID
	 WHERE id.itemID = i.itemID AND f.fieldName = ?)
`

// baseSelect returns a SELECT that pulls common display columns for a list
// of items. The result row order is:
//
//	itemID, key, typeName, version, dateAdded, dateModified, title, date, DOI, publicationTitle
//
// Callers append WHERE/ORDER BY/LIMIT.
func baseSelect() string {
	return `
SELECT i.itemID, i.key, it.typeName, i.version, i.dateAdded, i.clientDateModified,
	` + fieldValueSubquery + ` AS title,
	` + fieldValueSubquery + ` AS date,
	` + fieldValueSubquery + ` AS doi,
	` + fieldValueSubquery + ` AS pub
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
`
}

// scanListRow scans a baseSelect() row into an Item.
func scanListRow(rows *sql.Rows) (Item, error) {
	var it Item
	var title, date, doi, pub sql.NullString
	if err := rows.Scan(
		&it.ID, &it.Key, &it.Type, &it.Version, &it.DateAdded, &it.DateModified,
		&title, &date, &doi, &pub,
	); err != nil {
		return it, err
	}
	it.Title = title.String
	it.Date = date.String
	it.Year = ParseYear(it.Date)
	it.DOI = doi.String
	it.Publication = pub.String
	return it, nil
}

// listArgs returns the 4 field-name params baseSelect() expects for its
// correlated subqueries (one per fieldValueSubquery occurrence).
func listArgs() []any { return []any{"title", "date", "DOI", "publicationTitle"} }

// List returns items matching the filter, with metadata but no creators/tags/
// collections/attachments (use Read for those).
func (d *DB) List(f ListFilter) ([]Item, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}

	var (
		where strings.Builder
		args  []any
	)
	args = append(args, listArgs()...)
	where.WriteString(" WHERE i.libraryID = ? AND di.itemID IS NULL ")
	args = append(args, d.libraryID)
	// Skip the blanket note/attachment exclusion when the caller explicitly
	// asked for one of those types — otherwise the two clauses contradict
	// each other and we silently return zero rows.
	if !isExcludedContentType(f.ItemType) {
		where.WriteString(contentItemTypeFilter)
	}

	if f.ItemType != "" {
		where.WriteString(" AND it.typeName = ? ")
		args = append(args, f.ItemType)
	}
	if f.CollectionKey != "" {
		where.WriteString(` AND i.itemID IN (
			SELECT ci.itemID FROM collectionItems ci
			JOIN collections c ON ci.collectionID = c.collectionID
			WHERE c.key = ? AND c.libraryID = ?
		) `)
		args = append(args, f.CollectionKey, d.libraryID)
	}
	if f.Tag != "" {
		where.WriteString(` AND i.itemID IN (
			SELECT it2.itemID FROM itemTags it2
			JOIN tags tg ON it2.tagID = tg.tagID
			WHERE tg.name = ?
		) `)
		args = append(args, f.Tag)
	}

	order := " ORDER BY i.dateAdded DESC "
	switch f.OrderBy {
	case OrderDateModifiedDesc:
		order = " ORDER BY i.clientDateModified DESC "
	case OrderTitleAsc:
		// Sort by the pulled title subquery; NULLs last.
		order = " ORDER BY title IS NULL, title COLLATE NOCASE ASC "
	}

	q := baseSelect() + where.String() + order + " LIMIT ? OFFSET ? "
	args = append(args, limit, f.Offset)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		it, err := scanListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListAll returns every item matching the filter, fully hydrated with
// Fields and Creators. Unlike List (which is paginated and metadata-only),
// this is intended for bulk export — no default LIMIT, but callers can still
// cap via f.Limit if they want.
//
// Hydration is done with two follow-up queries (one per batch for fields,
// one per batch for creators), not per-item round-trips. On the live 7300-
// item library this keeps the whole export under a second.
func (d *DB) ListAll(f ListFilter) ([]Item, error) {
	var (
		where strings.Builder
		args  []any
	)
	args = append(args, listArgs()...)
	where.WriteString(" WHERE i.libraryID = ? AND di.itemID IS NULL ")
	args = append(args, d.libraryID)
	// Skip the blanket note/attachment exclusion when the caller explicitly
	// asked for one of those types — otherwise the two clauses contradict
	// each other and we silently return zero rows.
	if !isExcludedContentType(f.ItemType) {
		where.WriteString(contentItemTypeFilter)
	}

	if f.ItemType != "" {
		where.WriteString(" AND it.typeName = ? ")
		args = append(args, f.ItemType)
	}
	if f.CollectionKey != "" {
		where.WriteString(` AND i.itemID IN (
			SELECT ci.itemID FROM collectionItems ci
			JOIN collections c ON ci.collectionID = c.collectionID
			WHERE c.key = ? AND c.libraryID = ?
		) `)
		args = append(args, f.CollectionKey, d.libraryID)
	}
	if f.Tag != "" {
		where.WriteString(` AND i.itemID IN (
			SELECT it2.itemID FROM itemTags it2
			JOIN tags tg ON it2.tagID = tg.tagID
			WHERE tg.name = ?
		) `)
		args = append(args, f.Tag)
	}

	q := baseSelect() + where.String() + " ORDER BY i.itemID ASC "
	if f.Limit > 0 {
		q += " LIMIT ? "
		args = append(args, f.Limit)
	}

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list all items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	idIndex := map[int64]int{}
	for rows.Next() {
		it, err := scanListRow(rows)
		if err != nil {
			return nil, err
		}
		idIndex[it.ID] = len(out)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	// Bulk-hydrate Fields in one query across every returned item.
	ids := lo.Map(out, func(it Item, _ int) int64 { return it.ID })
	if err := d.hydrateFields(out, idIndex, ids); err != nil {
		return nil, err
	}
	if err := d.hydrateCreators(out, idIndex, ids); err != nil {
		return nil, err
	}
	// Surface the denormalized URL/abstract that Read() would have set.
	for i := range out {
		out[i].URL = out[i].Fields["url"]
		out[i].Abstract = out[i].Fields["abstractNote"]
	}
	return out, nil
}

// hydrateFields populates Item.Fields for every row in out, keyed via
// idIndex (itemID → position). One query regardless of batch size.
func (d *DB) hydrateFields(out []Item, idIndex map[int64]int, ids []int64) error {
	placeholders, args := inClause(ids)
	q := `
		SELECT id.itemID, f.fieldName, idv.value
		FROM itemData id
		JOIN fields f ON id.fieldID = f.fieldID
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE id.itemID IN (` + placeholders + `)
	`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return fmt.Errorf("hydrate fields: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var itemID int64
		var name, val string
		if err := rows.Scan(&itemID, &name, &val); err != nil {
			return err
		}
		idx, ok := idIndex[itemID]
		if !ok {
			continue
		}
		if out[idx].Fields == nil {
			out[idx].Fields = map[string]string{}
		}
		out[idx].Fields[name] = val
	}
	return rows.Err()
}

// hydrateCreators populates Item.Creators for every row in out. One query.
func (d *DB) hydrateCreators(out []Item, idIndex map[int64]int, ids []int64) error {
	placeholders, args := inClause(ids)
	q := `
		SELECT ic.itemID, ct.creatorType, c.firstName, c.lastName, c.fieldMode, ic.orderIndex
		FROM itemCreators ic
		JOIN creators c ON ic.creatorID = c.creatorID
		JOIN creatorTypes ct ON ic.creatorTypeID = ct.creatorTypeID
		WHERE ic.itemID IN (` + placeholders + `)
		ORDER BY ic.itemID, ic.orderIndex
	`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return fmt.Errorf("hydrate creators: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var itemID int64
		var cr Creator
		var first, last sql.NullString
		var mode int
		if err := rows.Scan(&itemID, &cr.Type, &first, &last, &mode, &cr.OrderIdx); err != nil {
			return err
		}
		if mode == 1 {
			cr.Name = last.String
		} else {
			cr.First = first.String
			cr.Last = last.String
		}
		idx, ok := idIndex[itemID]
		if !ok {
			continue
		}
		out[idx].Creators = append(out[idx].Creators, cr)
	}
	return rows.Err()
}

// inClause builds a `?,?,?,…` placeholder list and a matching []any args
// slice for a SQL IN (...) expression.
func inClause(ids []int64) (string, []any) {
	ph := lo.Map(ids, func(_ int64, _ int) string { return "?" })
	args := lo.Map(ids, func(id int64, _ int) any { return id })
	return strings.Join(ph, ","), args
}

// GetItemsByKeys returns a narrow snapshot (Key + Version + Type + Collections)
// for every requested key that exists in the current library. Missing,
// trashed, and out-of-scope keys are silently omitted — callers that need
// to report "not found" should diff the input against the returned keys.
//
// Runs two queries regardless of |keys|: one for the core columns, one
// to hydrate Collections. This is the bulk primitive behind batch write
// paths (e.g. `zot collection add --from-file`) that populate ItemPatch
// so UpdateItemsBatch can skip per-item GETs.
func (d *DB) GetItemsByKeys(keys []string) ([]Item, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders, keyArgs := inClauseStrings(keys)
	args := []any{d.libraryID}
	args = append(args, keyArgs...)

	q := `
SELECT i.itemID, i.key, i.version, it.typeName
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ? AND di.itemID IS NULL AND i.key IN (` + placeholders + `)
`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("get items by keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	idIndex := map[int64]int{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Key, &it.Version, &it.Type); err != nil {
			return nil, err
		}
		idIndex[it.ID] = len(out)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	// Hydrate Collections in one query across every returned item.
	ids := lo.Map(out, func(it Item, _ int) int64 { return it.ID })
	collPh, collArgs := inClause(ids)
	crows, err := d.db.Query(`
		SELECT ci.itemID, c.key
		FROM collectionItems ci
		JOIN collections c ON ci.collectionID = c.collectionID
		WHERE ci.itemID IN (`+collPh+`)
	`, collArgs...)
	if err != nil {
		return nil, fmt.Errorf("hydrate collections: %w", err)
	}
	defer func() { _ = crows.Close() }()
	for crows.Next() {
		var itemID int64
		var ck string
		if err := crows.Scan(&itemID, &ck); err != nil {
			return nil, err
		}
		if idx, ok := idIndex[itemID]; ok {
			out[idx].Collections = append(out[idx].Collections, ck)
		}
	}
	return out, crows.Err()
}

// inClauseStrings mirrors inClause for []string args — used when the IN clause
// binds Zotero keys rather than internal itemIDs.
func inClauseStrings(keys []string) (string, []any) {
	ph := lo.Map(keys, func(_ string, _ int) string { return "?" })
	args := lo.Map(keys, func(k string, _ int) any { return k })
	return strings.Join(ph, ","), args
}

// ItemKeysByDOI returns a map of DOI → Zotero key for every item in the
// library whose DOI matches one of the inputs. Lookup is case-insensitive
// (DOIs are case-insensitive per RFC 7595, but Zotero stores them as the
// user typed them). Returns an empty map for an empty input.
//
// Used by graph traversal to figure out which OpenAlex referenced/citing
// works are already in the user's library, so the agent-facing JSON can
// split the result into in_library vs outside_library buckets.
func (d *DB) ItemKeysByDOI(dois []string) (map[string]string, error) {
	if len(dois) == 0 {
		return map[string]string{}, nil
	}

	// Normalize input keys to lowercase for the result map; SQL match is
	// case-insensitive via LOWER() on both sides. Zotero indexes itemData
	// values raw, so a covering index on LOWER(value) doesn't exist —
	// expect a scan, but it's only over the DOI subset.
	lowered := lo.Map(dois, func(d string, _ int) string { return strings.ToLower(strings.TrimSpace(d)) })
	placeholders, args := inClauseStrings(lowered)

	q := `
SELECT i.key, idv.value
FROM items i
JOIN itemData id ON id.itemID = i.itemID
JOIN fields f ON id.fieldID = f.fieldID
JOIN itemDataValues idv ON id.valueID = idv.valueID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
WHERE i.libraryID = ?
  AND di.itemID IS NULL
  AND f.fieldName = 'DOI'
  AND LOWER(idv.value) IN (` + placeholders + `)
`
	full := slices.Concat([]any{d.libraryID}, args)
	rows, err := d.db.Query(q, full...)
	if err != nil {
		return nil, fmt.Errorf("ItemKeysByDOI: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[strings.ToLower(value)] = key
	}
	return out, rows.Err()
}

// Search returns items matching the query. The query is parsed by
// [match.ParseClauses], which supports:
//
//   - free text:        "neuroimaging"           (matches title/doi/pub/creator)
//   - field scope:      "@author: jolly"
//   - AND clauses:      "@author: jolly @title: gossip"   (comma optional)
//   - OR groups:        "@type: book | @type: thesis"
//   - negation:         "@author: -smith"
//
// Recognized fields: author/creator, title, doi, pub/publication, tag, type/
// itemType, year. Smartcase applies per-clause: an all-lowercase needle is
// matched case-insensitively, any uppercase flips it to case-sensitive.
// Zotero has no FTS on EAV metadata — every clause is a table scan.
func (d *DB) Search(query string, limit int) ([]Item, error) {
	if limit == 0 {
		limit = 50
	}
	groups := match.ParseClauses(query)
	if len(groups) == 0 {
		return nil, nil
	}

	var orParts []string
	var clauseArgs []any
	for _, group := range groups {
		var andParts []string
		for _, c := range group {
			frag, fa, err := buildClauseSQL(c)
			if err != nil {
				return nil, err
			}
			if frag == "" {
				continue
			}
			andParts = append(andParts, frag)
			clauseArgs = append(clauseArgs, fa...)
		}
		if len(andParts) > 0 {
			orParts = append(orParts, "("+strings.Join(andParts, " AND ")+")")
		}
	}
	if len(orParts) == 0 {
		return nil, nil
	}

	// Use a CTE so clause fragments can reference the pulled title/doi/pub/
	// date/typeName columns directly via the `b` alias.
	q := `
WITH base AS (` + baseSelect() + `
	WHERE i.libraryID = ? AND di.itemID IS NULL ` + contentItemTypeFilter + `
)
SELECT b.* FROM base b
WHERE ` + strings.Join(orParts, " OR ") + `
ORDER BY b.dateAdded DESC
LIMIT ?
`
	args := listArgs()
	args = append(args, d.libraryID)
	args = append(args, clauseArgs...)
	args = append(args, limit)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		it, err := scanListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// buildClauseSQL converts a single parsed clause into a SQL WHERE fragment
// (with `?` placeholders) and the args to bind. The fragment is meant to be
// composed under `SELECT b.* FROM base b` — column references go through the
// `b` alias. Returns an error for unknown field names so typos surface
// instead of silently expanding the result set.
func buildClauseSQL(c match.Clause) (string, []any, error) {
	if c.Terms == "" {
		if c.Column == "" {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("empty value for field %q", c.Column)
	}

	needle := c.Terms
	smartcase := needle == strings.ToLower(needle)
	if smartcase {
		needle = strings.ToLower(needle)
	}
	fold := func(expr string) string {
		if smartcase {
			return "lower(" + expr + ")"
		}
		return expr
	}

	creatorExpr := "(c.firstName || ' ' || c.lastName)"
	creatorExists := "EXISTS (SELECT 1 FROM itemCreators ic" +
		" JOIN creators c ON ic.creatorID = c.creatorID" +
		" WHERE ic.itemID = b.itemID AND instr(" + fold(creatorExpr) + ", ?) > 0)"

	var frag string
	var args []any
	switch strings.ToLower(c.Column) {
	case "":
		frag = "(instr(" + fold("b.title") + ", ?) > 0" +
			" OR instr(" + fold("b.doi") + ", ?) > 0" +
			" OR instr(" + fold("b.pub") + ", ?) > 0" +
			" OR " + creatorExists + ")"
		args = []any{needle, needle, needle, needle}
	case "title":
		frag = "instr(" + fold("b.title") + ", ?) > 0"
		args = []any{needle}
	case "doi":
		frag = "instr(" + fold("b.doi") + ", ?) > 0"
		args = []any{needle}
	case "pub", "publication":
		frag = "instr(" + fold("b.pub") + ", ?) > 0"
		args = []any{needle}
	case "author", "creator":
		frag = creatorExists
		args = []any{needle}
	case "tag":
		frag = "EXISTS (SELECT 1 FROM itemTags ity" +
			" JOIN tags tg ON ity.tagID = tg.tagID" +
			" WHERE ity.itemID = b.itemID AND instr(" + fold("tg.name") + ", ?) > 0)"
		args = []any{needle}
	case "type", "itemtype":
		// Type names are stable lowercase identifiers (journalArticle, book…);
		// equality reads better than substring and avoids `book` matching
		// `bookSection`.
		frag = "lower(b.typeName) = ?"
		args = []any{strings.ToLower(c.Terms)}
	case "year":
		// Zotero stores dates as "YYYY-MM-DD …" with a sortable prefix even
		// when the user only typed a year (year-only is "YYYY-00-00 YYYY").
		// First 4 chars are always the year.
		frag = "substr(b.date, 1, 4) = ?"
		args = []any{c.Terms}
	default:
		return "", nil, fmt.Errorf(
			"unknown search field %q (valid: author, title, doi, pub, tag, type, year)",
			c.Column,
		)
	}

	if c.Negate {
		frag = "NOT (" + frag + ")"
	}
	return frag, args, nil
}

// Read returns a single item by 8-char Zotero key, fully hydrated with
// creators, tags, collections, and attachments.
func (d *DB) Read(key string) (*Item, error) {
	args := listArgs()
	args = append(args, d.libraryID, key)
	q := baseSelect() + `
WHERE i.libraryID = ? AND di.itemID IS NULL AND i.key = ?
LIMIT 1
`
	row := d.db.QueryRow(q, args...)
	var it Item
	var title, date, doi, pub sql.NullString
	if err := row.Scan(
		&it.ID, &it.Key, &it.Type, &it.Version, &it.DateAdded, &it.DateModified,
		&title, &date, &doi, &pub,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("item %s not found", key)
		}
		return nil, err
	}
	it.Title = title.String
	it.Date = date.String
	it.Year = ParseYear(it.Date)
	it.DOI = doi.String
	it.Publication = pub.String

	// Pull all fields into the Fields map.
	fields, err := d.itemFields(it.ID)
	if err != nil {
		return nil, err
	}
	it.Fields = fields
	it.URL = fields["url"]
	it.Abstract = fields["abstractNote"]
	it.Extra = fields["extra"]

	creators, err := d.itemCreators(it.ID)
	if err != nil {
		return nil, err
	}
	it.Creators = creators

	tags, err := d.itemTags(it.ID)
	if err != nil {
		return nil, err
	}
	it.Tags = tags

	colls, err := d.itemCollectionKeys(it.ID)
	if err != nil {
		return nil, err
	}
	it.Collections = colls

	atts, err := d.itemAttachments(it.ID)
	if err != nil {
		return nil, err
	}
	it.Attachments = atts

	return &it, nil
}

// itemFields returns the complete EAV field map for an item.
func (d *DB) itemFields(itemID int64) (map[string]string, error) {
	rows, err := d.db.Query(`
		SELECT f.fieldName, idv.value
		FROM itemData id
		JOIN fields f ON id.fieldID = f.fieldID
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE id.itemID = ?
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var name, val string
		if err := rows.Scan(&name, &val); err != nil {
			return nil, err
		}
		out[name] = val
	}
	return out, rows.Err()
}

func (d *DB) itemCreators(itemID int64) ([]Creator, error) {
	rows, err := d.db.Query(`
		SELECT ct.creatorType, c.firstName, c.lastName, c.fieldMode, ic.orderIndex
		FROM itemCreators ic
		JOIN creators c ON ic.creatorID = c.creatorID
		JOIN creatorTypes ct ON ic.creatorTypeID = ct.creatorTypeID
		WHERE ic.itemID = ?
		ORDER BY ic.orderIndex
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Creator
	for rows.Next() {
		var cr Creator
		var first, last sql.NullString
		var mode int
		if err := rows.Scan(&cr.Type, &first, &last, &mode, &cr.OrderIdx); err != nil {
			return nil, err
		}
		if mode == 1 {
			cr.Name = last.String // Zotero stores single-name creators in lastName
		} else {
			cr.First = first.String
			cr.Last = last.String
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

func (d *DB) itemTags(itemID int64) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT tg.name
		FROM itemTags it
		JOIN tags tg ON it.tagID = tg.tagID
		WHERE it.itemID = ?
		ORDER BY tg.name
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (d *DB) itemCollectionKeys(itemID int64) ([]string, error) {
	rows, err := d.db.Query(`
		SELECT c.key
		FROM collectionItems ci
		JOIN collections c ON ci.collectionID = c.collectionID
		WHERE ci.itemID = ?
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (d *DB) itemAttachments(parentID int64) ([]Attachment, error) {
	rows, err := d.db.Query(`
		SELECT ch.key, ia.contentType, ia.path, ia.linkMode
		FROM itemAttachments ia
		JOIN items ch ON ia.itemID = ch.itemID
		WHERE ia.parentItemID = ?
		ORDER BY ch.dateAdded
	`, parentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Attachment
	for rows.Next() {
		var a Attachment
		var ct, path sql.NullString
		if err := rows.Scan(&a.Key, &ct, &path, &a.LinkMode); err != nil {
			return nil, err
		}
		a.ContentType = ct.String
		// Zotero stores attachment paths as "storage:filename.pdf".
		p := path.String
		if strings.HasPrefix(p, "storage:") {
			a.Filename = strings.TrimPrefix(p, "storage:")
		} else {
			a.Filename = p
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Stats returns a library-wide summary.
func (d *DB) Stats() (*Stats, error) {
	s := &Stats{ByType: map[string]int{}}

	// Total + by type (content items only).
	rows, err := d.db.Query(`
		SELECT it.typeName, COUNT(*)
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ? AND di.itemID IS NULL
		  AND it.typeName NOT IN ('attachment','note')
		GROUP BY it.typeName
	`, d.libraryID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		s.ByType[name] = n
		s.TotalItems += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// With DOI / abstract.
	if err := d.countFieldPresent("DOI", &s.WithDOI); err != nil {
		return nil, err
	}
	if err := d.countFieldPresent("abstractNote", &s.WithAbstract); err != nil {
		return nil, err
	}

	// Items with at least one attachment.
	if err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT ia.parentItemID)
		FROM itemAttachments ia
		JOIN items p ON ia.parentItemID = p.itemID
		LEFT JOIN deletedItems di ON p.itemID = di.itemID
		WHERE p.libraryID = ? AND di.itemID IS NULL
	`, d.libraryID).Scan(&s.WithAttachment); err != nil {
		return nil, err
	}

	// Collections + tags counts.
	if err := d.db.QueryRow(
		`SELECT COUNT(*) FROM collections WHERE libraryID = ?`, d.libraryID,
	).Scan(&s.Collections); err != nil {
		return nil, err
	}
	if err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT tg.tagID)
		FROM tags tg
		JOIN itemTags it ON tg.tagID = it.tagID
		JOIN items i ON it.itemID = i.itemID
		WHERE i.libraryID = ?
	`, d.libraryID).Scan(&s.Tags); err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) countFieldPresent(fieldName string, out *int) error {
	return d.db.QueryRow(`
		SELECT COUNT(DISTINCT i.itemID)
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID
		LEFT JOIN deletedItems di ON i.itemID = di.itemID
		WHERE i.libraryID = ? AND di.itemID IS NULL
		  AND it.typeName NOT IN ('attachment','note')
		  AND f.fieldName = ?
	`, d.libraryID, fieldName).Scan(out)
}
