package local

import "fmt"

// Group is one Zotero group library as the local database records it.
//
// Two numbering systems meet here and both matter: GroupID is the Zotero Web
// API group id (the number in a `/groups/{id}/…` URL, and what
// zot.Config.SharedGroupID stores), while LibraryID is the SQLite
// `libraries.libraryID` every local query filters on. [ForGroupByAPIID]
// bridges the two.
type Group struct {
	GroupID   int64  `json:"group_id"`
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name"`
}

// Groups lists every group library the local Zotero database holds, ordered
// by name.
//
// This is where the shared-library scope comes from. Naming a group is a
// local fact — the `groups` table carries it — so resolving `--library
// shared` needs no credential and no network.
//
// The result is independent of which library the handle is pinned to — a
// personal-scoped handle lists groups just as well.
func (d *DB) Groups() ([]Group, error) {
	rows, err := d.db.Query(
		"SELECT g.groupID, g.libraryID, g.name FROM groups g " +
			"JOIN libraries l ON l.libraryID = g.libraryID AND l.type='group' " +
			"ORDER BY g.name")
	if err != nil {
		return nil, fmt.Errorf("list Zotero groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.GroupID, &g.LibraryID, &g.Name); err != nil {
			return nil, fmt.Errorf("scan Zotero group: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
