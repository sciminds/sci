package local

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

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
//	itemID, key, typeName, version, dateAdded, dateModified, title, date, DOI, publicationTitle, citationKey
//
// Callers append WHERE/ORDER BY/LIMIT.
func baseSelect() string {
	return `
SELECT i.itemID, i.key, i.libraryID, it.typeName, i.version, i.dateAdded, i.clientDateModified,
	` + fieldValueSubquery + ` AS title,
	` + fieldValueSubquery + ` AS date,
	` + fieldValueSubquery + ` AS doi,
	` + fieldValueSubquery + ` AS pub,
	` + fieldValueSubquery + ` AS citekey
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
`
}

// scanListRow scans a baseSelect() row into an Item, stamping Library
// from the row's own libraryID — under a merged (ForAll) handle that is
// the row's provenance; under a single-library handle it's a constant.
func (d *DB) scanListRow(rows *sql.Rows) (Item, error) {
	var it Item
	var libID int64
	var title, date, doi, pub, citekey sql.NullString
	if err := rows.Scan(
		&it.ID, &it.Key, &libID, &it.Type, &it.Version, &it.DateAdded, &it.DateModified,
		&title, &date, &doi, &pub, &citekey,
	); err != nil {
		return it, err
	}
	it.Library = d.scopeLabel(libID)
	it.Title = title.String
	it.Date = date.String
	it.Year = ParseYear(it.Date)
	it.DOI = doi.String
	it.Publication = pub.String
	it.Citekey = citekey.String
	return it, nil
}

// listArgs returns the 5 field-name params baseSelect() expects for its
// correlated subqueries (one per fieldValueSubquery occurrence).
func listArgs() []any { return []any{"title", "date", "DOI", "publicationTitle", "citationKey"} }

// listWhere builds the shared WHERE fragment for List / ListAll / CountList
// from a filter. The returned args start at the libraryID parameter — callers
// whose SELECT uses baseSelect() must prepend listArgs() for its correlated
// subqueries.
func (d *DB) listWhere(f ListFilter) (string, []any) {
	var (
		where strings.Builder
		args  []any
	)
	libFrag, libArgs := d.libIn("i")
	where.WriteString(" WHERE " + libFrag + " AND di.itemID IS NULL ")
	args = append(args, libArgs...)
	// Skip the blanket note/attachment exclusion when the caller explicitly
	// asked for one of those types — otherwise the two clauses contradict
	// each other and we silently return zero rows.
	if !isExcludedContentType(f.ItemType) {
		if f.Mirror {
			where.WriteString(contentItemTypeFilter)
		} else {
			where.WriteString(hygieneItemTypeFilter)
		}
	}

	if f.ItemType != "" {
		where.WriteString(" AND it.typeName = ? ")
		args = append(args, f.ItemType)
	}
	if f.CollectionKey != "" {
		colFrag, colArgs := d.libIn("c")
		where.WriteString(` AND i.itemID IN (
			SELECT ci.itemID FROM collectionItems ci
			JOIN collections c ON ci.collectionID = c.collectionID
			WHERE c.key = ? AND ` + colFrag + `
		) `)
		args = append(args, f.CollectionKey)
		args = append(args, colArgs...)
	}
	if f.Tag != "" {
		where.WriteString(` AND i.itemID IN (
			SELECT it2.itemID FROM itemTags it2
			JOIN tags tg ON it2.tagID = tg.tagID
			WHERE tg.name = ?
		) `)
		args = append(args, f.Tag)
	}
	if len(f.Keys) > 0 {
		ph, keyArgs := inClauseStrings(f.Keys)
		where.WriteString(" AND i.key IN (" + ph + ") ")
		args = append(args, keyArgs...)
	}
	return where.String(), args
}

// CountList returns the total number of items matching the filter, ignoring
// Limit/Offset — the honest denominator for a truncated List page.
func (d *DB) CountList(f ListFilter) (int, error) {
	where, args := d.listWhere(f)
	q := `
SELECT COUNT(*)
FROM items i
JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
LEFT JOIN deletedItems di ON i.itemID = di.itemID
` + where
	var n int
	if err := d.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count items: %w", err)
	}
	return n, nil
}

// List returns items matching the filter, with metadata but no creators/tags/
// collections/attachments (use Read for those).
func (d *DB) List(f ListFilter) ([]Item, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}

	where, whereArgs := d.listWhere(f)
	args := append(listArgs(), whereArgs...)

	order := " ORDER BY i.dateAdded DESC "
	switch f.OrderBy {
	case OrderDateModifiedDesc:
		order = " ORDER BY i.clientDateModified DESC "
	case OrderTitleAsc:
		// Sort by the pulled title subquery; NULLs last.
		order = " ORDER BY title IS NULL, title COLLATE NOCASE ASC "
	}

	q := baseSelect() + where + order + " LIMIT ? OFFSET ? "
	args = append(args, limit, f.Offset)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		it, err := d.scanListRow(rows)
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
	// Set here rather than by the caller: this method is what the NDJSON
	// mirror is built from, and losslessness should not depend on every
	// call site remembering a flag.
	f.Mirror = true
	where, whereArgs := d.listWhere(f)
	args := append(listArgs(), whereArgs...)

	q := baseSelect() + where + " ORDER BY i.itemID ASC "
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
		it, err := d.scanListRow(rows)
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

// SearchOptions tunes SearchWith behavior beyond the query string.
type SearchOptions struct {
	// Content, when non-nil, widens positive free-text clauses with the
	// items whose paper text matches — see internal/zot/content. It is
	// called once per OR group and returns each matching item key with
	// its relevance score (higher is better).
	//
	// The scores are not decoration: content hits carry no title
	// relevance, so without them the whole tail of a broad search ties
	// and falls back to year — newest-wins, which answers a different
	// question than the one that was asked.
	//
	// It receives the group's free text verbatim, quotes and all, rather
	// than a word list: the content index understands "quoted phrases"
	// and prefix*, and splitting the query into words here would throw
	// that away before the index ever saw it.
	//
	// Field-scoped and negated clauses never consult it, matching how
	// the metadata search treats them.
	Content func(text string) (map[string]float64, error)
}

// Search returns items matching the query, ranked by title relevance.
// Shorthand for [DB.SearchWith] with zero options.
func (d *DB) Search(query string, limit int) ([]Item, error) {
	return d.SearchWith(query, limit, SearchOptions{})
}

// SearchWith returns items matching the query. The query is parsed by
// [match.ParseClauses], which supports:
//
//   - free text:        "neuroimaging"           (matches title/doi/pub/creator/citekey)
//   - field scope:      "@author: jolly"          (bare "author: jolly" works too)
//   - AND clauses:      "@author: jolly @title: gossip"   (comma optional)
//   - OR groups:        "@type: book | @type: thesis"
//   - negation:         "@author: -smith"         (bare "-author:smith" works too)
//
// The bare forms are rewritten by [NormalizeQuery] before parsing, and
// positive free text is word-ANDed by [expandFreeText]: each word must
// match SOME field, so "jolly 2021" finds a 2021 paper by Jolly even
// though no single column contains that string. A bare 1500-2099 token
// means the publication year; a "quoted phrase" stays one literal
// substring (and quoting is how a year-like title such as "2001" stays
// text). Negated free text is NOT split — it excludes the phrase as
// typed.
//
// Recognized fields: author/creator, title, doi, pub/publication, tag, type/
// itemType, year, citekey/key. Smartcase applies per-clause — after word
// splitting, that means per word: an all-lowercase needle is matched
// case-insensitively, any uppercase flips it to case-sensitive. Zotero
// has no FTS on EAV metadata — every clause is a table scan.
//
// Results are ordered by title relevance (how many positive query words
// appear in the title), then by content relevance when
// [SearchOptions.Content] supplied scores, then year descending, then
// dateAdded descending; limit applies after ranking so a broad query
// can't crowd out the most relevant hit.
func (d *DB) SearchWith(query string, limit int, opts SearchOptions) ([]Item, error) {
	items, _, err := d.SearchWithTotal(query, limit, opts)
	return items, err
}

// bareFieldTokenRe matches a query token carrying a recognized field
// prefix without the @ sigil — `tag:cats`, `-year:2023` — including the
// bare `tag:` form whose value is the following token. The field list
// mirrors buildClauseSQL's switch exactly; an unrecognized prefix
// (`re:thinking`) stays free text instead of erroring, because bare
// syntax has no sigil declaring "I meant a field".
var bareFieldTokenRe = regexp.MustCompile(`(?i)^(-?)(author|creator|title|doi|pub|publication|tag|type|itemtype|year|citekey|key):(.*)$`)

// NormalizeQuery rewrites bare field prefixes into the @field: form
// [match.ParseClauses] understands: `tag:cats` → `@tag: cats`, and a
// clause-leading `-tag:cats` → `@tag: -cats`, where applyNegate already
// picks the `-` up off the value. This is the compatibility shim that
// makes sci the single owner of the search DSL — downstream consumers
// (zen) write the bare form, and the two dialects were always
// semantically identical where their syntax overlapped.
//
// Quoted spans are opaque: a colon inside `"attention: review"` is
// prose, not a field. A detached `-tag:` (negation with the value in the
// next token) has nowhere to hang the `-` and passes through as free
// text. Inter-token whitespace is collapsed to single spaces — the
// clause splitter never depended on it.
func NormalizeQuery(query string) string {
	inQuote := false
	toks := lo.Map(strings.Fields(query), func(tok string, _ int) string {
		rewritten := tok
		if !inQuote {
			if m := bareFieldTokenRe.FindStringSubmatch(tok); m != nil {
				neg, field, val := m[1], m[2], m[3]
				switch {
				case val == "" && neg == "-":
					// nowhere to hang the negation — leave as free text
				case val == "":
					rewritten = "@" + field + ":"
				default:
					rewritten = "@" + field + ": " + neg + val
				}
			}
		}
		if strings.Count(tok, `"`)%2 == 1 {
			inQuote = !inQuote
		}
		return rewritten
	})
	return strings.Join(toks, " ")
}

// SearchWithTotal is [DB.SearchWith] plus the pre-limit match count, so
// callers can report "showing N of M" instead of silently truncating.
func (d *DB) SearchWithTotal(query string, limit int, opts SearchOptions) ([]Item, int, error) {
	if limit == 0 {
		limit = 50
	}
	groups := match.ParseClauses(NormalizeQuery(query))
	if len(groups) == 0 {
		return nil, 0, nil
	}
	groups = lo.Map(groups, func(g []match.Clause, _ int) []match.Clause {
		return expandFreeText(g)
	})

	var orParts []string
	var clauseArgs []any
	// contentScores accumulates every group's hits so the ranker can see
	// them all; an item matched by two OR groups keeps its best score.
	contentScores := map[string]float64{}
	for _, group := range groups {
		// Content matches feed an "also match these item IDs" set that
		// buildClauseSQL ORs into positive free-text clauses.
		var ftIDs []int64
		if opts.Content != nil {
			if text := bareSearchText(group); text != "" {
				scored, err := opts.Content(text)
				if err != nil {
					return nil, 0, err
				}
				for key, score := range scored {
					contentScores[key] = max(contentScores[key], score)
				}
				// The index knows keys; the clause SQL matches on item
				// IDs, and resolving the two is this package's job —
				// the hook stays a pure "what matched, how well".
				ids, err := d.ItemIDsForKeys(lo.Keys(scored))
				if err != nil {
					return nil, 0, err
				}
				ftIDs = lo.Uniq(ids)
			}
		}
		var andParts []string
		for _, c := range group {
			frag, fa, err := buildClauseSQL(c, ftIDs)
			if err != nil {
				return nil, 0, err
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
		return nil, 0, nil
	}

	// Use a CTE so clause fragments can reference the pulled title/doi/pub/
	// date/typeName columns directly via the `b` alias. No SQL LIMIT — all
	// matches are fetched so ranking sees the full candidate set; the
	// dateAdded ordering is the stable tiebreak the ranker preserves.
	libFrag, libArgs := d.libIn("i")
	q := `
WITH base AS (` + baseSelect() + `
	WHERE ` + libFrag + ` AND di.itemID IS NULL ` + hygieneItemTypeFilter + `
)
SELECT b.* FROM base b
WHERE ` + strings.Join(orParts, " OR ") + `
ORDER BY b.dateAdded DESC
`
	args := listArgs()
	args = append(args, libArgs...)
	args = append(args, clauseArgs...)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Item
	for rows.Next() {
		it, err := d.scanListRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	ranked := rankSearchResults(out, groups, contentScores)
	top := ranked[:min(limit, len(ranked))]
	// Only the hits that survived ranking and the limit get enriched —
	// three batched IN queries over ≤limit rows, never the whole
	// candidate set.
	if err := d.enrichListRows(top); err != nil {
		return nil, 0, err
	}
	return top, len(ranked), nil
}

// enrichListRows adds Creators, Tags, and URL to list-view rows in place,
// via one batched query per facet. Search rows carry these so consumers
// can render authorship without an N+1 Read per hit; the heavyweight
// remainder (Abstract, the Fields map, attachments, relations) stays
// Read-only territory. Ordering matches Read: creators by orderIndex,
// tags by name.
func (d *DB) enrichListRows(items []Item) error {
	if len(items) == 0 {
		return nil
	}
	ids := lo.Map(items, func(it Item, _ int) int64 { return it.ID })
	ph, args := inClause(ids)
	byID := lo.SliceToMap(lo.Range(len(items)), func(i int) (int64, *Item) {
		return items[i].ID, &items[i]
	})

	creatorRows, err := d.db.Query(`
		SELECT ic.itemID, ct.creatorType, c.firstName, c.lastName, c.fieldMode, ic.orderIndex
		FROM itemCreators ic
		JOIN creators c ON ic.creatorID = c.creatorID
		JOIN creatorTypes ct ON ic.creatorTypeID = ct.creatorTypeID
		WHERE ic.itemID IN (`+ph+`)
		ORDER BY ic.itemID, ic.orderIndex
	`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = creatorRows.Close() }()
	for creatorRows.Next() {
		var itemID int64
		var cr Creator
		var first, last sql.NullString
		var mode int
		if err := creatorRows.Scan(&itemID, &cr.Type, &first, &last, &mode, &cr.OrderIdx); err != nil {
			return err
		}
		if mode == 1 {
			cr.Name = last.String // single-name creators live in lastName
		} else {
			cr.First = first.String
			cr.Last = last.String
		}
		if it, ok := byID[itemID]; ok {
			it.Creators = append(it.Creators, cr)
		}
	}
	if err := creatorRows.Err(); err != nil {
		return err
	}

	tagRows, err := d.db.Query(`
		SELECT ity.itemID, tg.name
		FROM itemTags ity
		JOIN tags tg ON ity.tagID = tg.tagID
		WHERE ity.itemID IN (`+ph+`)
		ORDER BY ity.itemID, tg.name
	`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = tagRows.Close() }()
	for tagRows.Next() {
		var itemID int64
		var name string
		if err := tagRows.Scan(&itemID, &name); err != nil {
			return err
		}
		if it, ok := byID[itemID]; ok {
			it.Tags = append(it.Tags, name)
		}
	}
	if err := tagRows.Err(); err != nil {
		return err
	}

	urlRows, err := d.db.Query(`
		SELECT id.itemID, idv.value
		FROM itemData id
		JOIN fields f ON id.fieldID = f.fieldID
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE f.fieldName = 'url' AND id.itemID IN (`+ph+`)
	`, args...)
	if err != nil {
		return err
	}
	defer func() { _ = urlRows.Close() }()
	for urlRows.Next() {
		var itemID int64
		var url string
		if err := urlRows.Scan(&itemID, &url); err != nil {
			return err
		}
		if it, ok := byID[itemID]; ok {
			it.URL = url
		}
	}
	return urlRows.Err()
}

// QueryFreeText returns the positive free-text of a query — every clause
// that is neither field-scoped nor negated — joined across OR groups,
// quoting preserved. It is "what the content index was asked", exposed so
// a caller that has already searched can ask the index a second question
// (snippets for the hits it decided to show) without re-parsing clauses.
// A query made only of field clauses returns "".
func QueryFreeText(query string) string {
	texts := lo.FilterMap(match.ParseClauses(NormalizeQuery(query)), func(group []match.Clause, _ int) (string, bool) {
		// The same expansion SearchWithTotal applies, so the snippet
		// query and the widening query ask the index one question:
		// year tokens are metadata filters, not paper-text evidence.
		text := bareSearchText(expandFreeText(group))
		return text, text != ""
	})
	return strings.Join(texts, " ")
}

// bareYearRe recognizes a free-text token that can only plausibly mean
// a publication year (1500-2099). Quoted tokens never reach this test —
// quoting is the escape hatch for titles like "2001".
var bareYearRe = regexp.MustCompile(`^(1[5-9]\d{2}|20\d{2})$`)

// expandFreeText rewrites each positive free-text clause in an AND
// group into per-token clauses: words must each match SOME field
// (creator + title + year can cooperate on "jolly 2021"), a bare year
// token becomes an @year: clause, and a quoted phrase stays one clause
// with its quotes preserved (buildClauseSQL strips them for the
// substring match; bareSearchText keeps them for the content index).
//
// Negated free text passes through unsplit on purpose: NOT("deep
// learning") excludes items containing the phrase, while splitting
// would exclude items containing either word — a different question.
// Field-scoped clauses are untouched; their value is a single needle.
func expandFreeText(group []match.Clause) []match.Clause {
	return lo.FlatMap(group, func(c match.Clause, _ int) []match.Clause {
		if c.Column != "" || c.Negate {
			return []match.Clause{c}
		}
		toks := splitFreeTextTokens(c.Terms)
		if len(toks) == 0 {
			return []match.Clause{c}
		}
		return lo.Map(toks, func(tok string, _ int) match.Clause {
			if bareYearRe.MatchString(tok) {
				return match.Clause{Column: "year", Terms: tok}
			}
			return match.Clause{Terms: tok}
		})
	})
}

// splitFreeTextTokens splits free text on whitespace, keeping a quoted
// span as one token (quotes included). An unterminated quote swallows
// the rest of the string — the user is mid-typing, not writing a
// program, so tolerance beats an error.
func splitFreeTextTokens(s string) []string {
	var toks []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case !inQuote && unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}

// stripOuterQuotes removes one matched pair of surrounding double
// quotes: the pair marks a phrase, and the quote characters themselves
// can never appear in the metadata being substring-matched.
func stripOuterQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// bareSearchText joins a group's positive free-text clauses back into
// one string, preserving the user's quoting.
//
// Handing the content index a word list instead would silently downgrade
// `"prediction error"` from a phrase to two ANDed terms — the index does
// its own parsing, and phrases are most of why it exists.
// (positiveQueryWords, further down, does split into words: title
// ranking counts word hits, so there the word is the unit.)
func bareSearchText(group []match.Clause) string {
	positive := lo.Filter(group, func(c match.Clause, _ int) bool {
		return c.Column == "" && !c.Negate && c.Terms != ""
	})
	terms := lo.Map(positive, func(c match.Clause, _ int) string { return c.Terms })
	return strings.TrimSpace(strings.Join(terms, " "))
}

// rankSearchResults orders hits by title relevance (the count of distinct
// positive query words appearing in the title, case-insensitive), then by
// content relevance, then year descending. The sort is stable, so
// equal-rank items keep the caller's dateAdded-descending order.
//
// Title before content is deliberate. A title match is the strongest
// evidence a library holds about what a paper is *about*, and items with
// no extraction have no content score at all — leading with bm25 would
// bury the paper the user named under every paper that mentions it in
// passing. Content relevance decides the long tail underneath, which is
// exactly where every hit ties at zero title words and the old fallback
// to year meant "newest wins".
func rankSearchResults(items []Item, groups [][]match.Clause, contentScores map[string]float64) []Item {
	words := positiveQueryWords(groups)
	if len(words) == 0 {
		return items
	}
	scores := lo.Map(items, func(it Item, _ int) int {
		title := strings.ToLower(it.Title)
		return lo.CountBy(words, func(w string) bool {
			return strings.Contains(title, w)
		})
	})
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(scores[b], scores[a]),
			cmp.Compare(contentScores[items[b].Key], contentScores[items[a].Key]),
			cmp.Compare(items[b].Year, items[a].Year),
		)
	})
	return lo.Map(idx, func(i int, _ int) Item { return items[i] })
}

// positiveQueryWords flattens every non-negated clause value across all OR
// groups into lowercase words. Field names don't matter for scoring — a
// term the user typed anywhere counts when it shows up in a title.
func positiveQueryWords(groups [][]match.Clause) []string {
	clauses := lo.Filter(lo.Flatten(groups), func(c match.Clause, _ int) bool {
		return !c.Negate && c.Terms != ""
	})
	words := lo.FlatMap(clauses, func(c match.Clause, _ int) []string {
		return strings.Fields(strings.ToLower(c.Terms))
	})
	// Quote characters glue themselves to a phrase's words; titles
	// contain the words, never the quotes.
	words = lo.FilterMap(words, func(w string, _ int) (string, bool) {
		w = strings.Trim(w, `"`)
		return w, w != ""
	})
	return lo.Uniq(words)
}

// synthKeySuffixRe extracts the trailing 8-char Zotero key from a pasted
// synthesized cite-key ({author}{year}-{words}-{ZOTKEY}) so whole-key
// lookups resolve even though synthesized keys are never stored in the DB.
// Case-insensitive: users may paste a lowercased key.
var synthKeySuffixRe = regexp.MustCompile(`(?i)-([a-z0-9]{8})$`)

// buildClauseSQL converts a single parsed clause into a SQL WHERE fragment
// (with `?` placeholders) and the args to bind. The fragment is meant to be
// composed under `SELECT b.* FROM base b` — column references go through the
// `b` alias. Returns an error for unknown field names so typos surface
// instead of silently expanding the result set.
//
// ftIDs, when non-empty, are fulltext-index hits that widen positive
// free-text clauses: the clause also matches any item whose itemID is in
// the set. Field-scoped and negated clauses ignore it.
func buildClauseSQL(c match.Clause, ftIDs []int64) (string, []any, error) {
	if c.Terms == "" {
		if c.Column == "" {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("empty value for field %q", c.Column)
	}

	// Quotes mark a phrase; the characters themselves would poison a
	// substring match. Stripped here (not in the parser) so
	// bareSearchText still hands the content index the quoted form.
	needle := stripOuterQuotes(c.Terms)
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
			" OR instr(" + fold("COALESCE(b.citekey, '')") + ", ?) > 0" +
			" OR " + creatorExists
		args = []any{needle, needle, needle, needle, needle}
		if !c.Negate && len(ftIDs) > 0 {
			ph, ftArgs := inClause(ftIDs)
			frag += " OR b.itemID IN (" + ph + ")"
			args = append(args, ftArgs...)
		}
		frag += ")"
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
	case "citekey", "key":
		// Matches the stored Zotero 7 citationKey field and the 8-char
		// Zotero item key (every synthesized cite-key embeds the latter
		// as its suffix). COALESCE keeps NULL citationKeys from poisoning
		// negated clauses (NOT(NULL) is NULL, which silently drops rows).
		frag = "(instr(" + fold("COALESCE(b.citekey, '')") + ", ?) > 0" +
			" OR instr(" + fold("b.key") + ", ?) > 0"
		args = []any{needle, needle}
		// A pasted whole synthesized key is longer than anything stored —
		// resolve it via its -ZOTKEY suffix instead.
		if m := synthKeySuffixRe.FindStringSubmatch(c.Terms); m != nil {
			frag += " OR b.key = ?"
			args = append(args, strings.ToUpper(m[1]))
		}
		frag += ")"
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
			"unknown search field %q (valid: author, title, doi, pub, tag, type, year, citekey)",
			c.Column,
		)
	}

	if c.Negate {
		frag = "NOT (" + frag + ")"
	}
	return frag, args, nil
}

// ItemNotFoundError reports that a key has no live row in the library
// this DB is scoped to — it is absent, trashed, or belongs to another
// library. Returned by [DB.Read] as a distinct type so callers can tell
// "this library doesn't have that item" from a query that actually
// failed; treating the two alike would let a broken DB masquerade as an
// empty one.
type ItemNotFoundError struct {
	Key string
}

// Error implements error.
func (e *ItemNotFoundError) Error() string { return "item " + e.Key + " not found" }

// Read returns a single item by 8-char Zotero key, fully hydrated with
// creators, tags, collections, and attachments. A key this library does
// not have yields an [ItemNotFoundError].
func (d *DB) Read(key string) (*Item, error) {
	libFrag, libArgs := d.libIn("i")
	args := listArgs()
	args = append(args, libArgs...)
	args = append(args, key)
	q := baseSelect() + `
WHERE ` + libFrag + ` AND di.itemID IS NULL AND i.key = ?
LIMIT 1
`
	row := d.db.QueryRow(q, args...)
	var it Item
	var libID int64
	var title, date, doi, pub, citekey sql.NullString
	if err := row.Scan(
		&it.ID, &it.Key, &libID, &it.Type, &it.Version, &it.DateAdded, &it.DateModified,
		&title, &date, &doi, &pub, &citekey,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &ItemNotFoundError{Key: key}
		}
		return nil, err
	}
	it.Library = d.scopeLabel(libID)
	it.Title = title.String
	it.Date = date.String
	it.Year = ParseYear(it.Date)
	it.DOI = doi.String
	it.Publication = pub.String
	it.Citekey = citekey.String

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

	// Relations stay nil when there are none, so the JSON shape of an
	// unrelated item is byte-identical to what it was before relations
	// existed. Labels come from one batched query, not one per far end.
	rels, err := d.ItemRelations(it.Key)
	if err != nil {
		return nil, err
	}
	if len(rels.Related) > 0 || len(rels.Other) > 0 {
		titles, err := d.ItemLabels(append(slices.Clone(rels.Related),
			lo.Flatten(lo.Values(rels.Other))...))
		if err != nil {
			return nil, err
		}
		if len(titles) > 0 {
			rels.Titles = titles
		}
		it.Relations = &rels
	}

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
		if after, ok := strings.CutPrefix(p, "storage:"); ok {
			a.Filename = after
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
		WHERE i.libraryID = ? AND di.itemID IS NULL `+
		hygieneItemTypeFilter+`
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
		WHERE i.libraryID = ? AND di.itemID IS NULL `+
		hygieneItemTypeFilter+`
		  AND f.fieldName = ?
	`, d.libraryID, fieldName).Scan(out)
}
