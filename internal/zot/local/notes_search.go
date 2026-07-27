package local

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/samber/lo"
)

// tagRe matches an HTML tag. Zotero stores note bodies as HTML, so every
// note is wrapped in markup that a naive text search would happily match.
var tagRe = regexp.MustCompile(`<[^>]*>`)

// SearchNotes returns the item IDs whose note content contains all of the
// given words — the parent item for a child note (which is what the ~5k
// docling extraction notes are), the note itself for a standalone one.
//
// Matching is case-insensitive substring over the note's *rendered text*,
// not its stored HTML. That distinction is the whole point: every Zotero
// note is wrapped in `<div class="zotero-note znv1">`, so a bare SQL LIKE
// makes "div", "class" and "znv1" match the entire library. SQL narrows the
// candidates with LIKE (fast, and wrong only in the permissive direction),
// then each candidate's markup is stripped and re-checked in Go.
//
// Returns nil, nil when words is empty.
func (d *DB) SearchNotes(words []string) ([]int64, error) {
	if len(words) == 0 {
		return nil, nil
	}

	// Stage 1 — SQL narrowing. Each word contributes at most one LIKE, on a
	// probe chosen to be permissive: it must not under-match, or stage 2
	// never sees the row. A word with no usable probe contributes nothing
	// and is enforced entirely by stage 2.
	var clauses []string
	args := []any{d.libraryID}
	for _, w := range words {
		probe := likeProbe(w)
		if probe == "" {
			continue
		}
		clauses = append(clauses, "n.note LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(probe)+"%")
	}
	if len(clauses) == 0 {
		clauses = append(clauses, "1=1")
	}

	q := `
SELECT n.itemID, COALESCE(n.parentItemID, n.itemID), n.note
FROM itemNotes n
JOIN items i ON i.itemID = n.itemID
LEFT JOIN deletedItems dn ON dn.itemID = n.itemID
LEFT JOIN deletedItems dp ON dp.itemID = n.parentItemID
WHERE i.libraryID = ?
  AND dn.itemID IS NULL
  AND dp.itemID IS NULL
  AND ` + strings.Join(clauses, "\n  AND ")

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search notes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	lowered := lo.Map(words, func(w string, _ int) string { return strings.ToLower(w) })

	var out []int64
	seen := map[int64]bool{}
	for rows.Next() {
		var noteID, targetID int64
		var body string
		if err := rows.Scan(&noteID, &targetID, &body); err != nil {
			return nil, err
		}
		// Stage 2 — markup stripped, entities decoded, then re-checked.
		text := strings.ToLower(NoteText(body))
		if !lo.EveryBy(lowered, func(w string) bool { return strings.Contains(text, w) }) {
			continue
		}
		if !seen[targetID] {
			seen[targetID] = true
			out = append(out, targetID)
		}
	}
	return out, rows.Err()
}

// NoteText renders a Zotero note's stored HTML down to searchable plain
// text: tags become spaces and entities are decoded.
//
// Tags become a space rather than nothing so `<h1>Title</h1><p>Body` can't
// weld "TitleBody" into a token that matches queries spanning neither word.
func NoteText(noteHTML string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(noteHTML, " "))), " ")
}

// likeProbe picks the longest fragment of a search word that is guaranteed
// to survive into the stored HTML verbatim, for use as the SQL narrowing
// filter. Returns "" when no fragment qualifies.
//
// Two things break a naive `LIKE '%word%'` against raw note HTML, and both
// break it in the dangerous direction — under-matching, which drops rows
// stage 2 would have accepted:
//
//   - Entity escaping. "communicability & predictive" is stored as
//     "communicability &amp; predictive", so the literal query never matches.
//   - Tag boundaries. "Representations The" is stored as
//     "Representations</h1><p>The".
//
// Splitting on whitespace and on every character HTML escapes leaves
// fragments that appear in the source byte-for-byte; the longest is the most
// selective. Correctness never rests on this — it only decides how many rows
// stage 2 has to strip and re-check.
func likeProbe(word string) string {
	fragments := strings.FieldsFunc(word, func(r rune) bool {
		return r == '&' || r == '<' || r == '>' || r == '"' || r == '\'' ||
			r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	return lo.MaxBy(fragments, func(a, b string) bool { return len(a) > len(b) })
}

// escapeLike neutralizes the SQL LIKE wildcards so a query containing % or _
// searches for those characters instead of globbing. Pairs with the
// `ESCAPE '\'` clause on every LIKE built here.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
