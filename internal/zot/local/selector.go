package local

import (
	"database/sql"
	"fmt"
)

// LibrarySelector chooses which row in the `libraries` table a local DB
// handle pins to. It is applied once during Open — every subsequent
// query filters on the resolved libraryID.
//
// Callers never construct a LibrarySelector directly; use ForPersonal
// or ForGroup. Zero value is invalid.
type LibrarySelector struct {
	resolve func(*sql.DB) (int64, error)
	label   string // for error messages
}

// ForPersonal selects the user's personal library (`libraries.type='user'`).
// There is always exactly one user library per Zotero account.
func ForPersonal() LibrarySelector {
	return LibrarySelector{
		label: "personal",
		resolve: func(db *sql.DB) (int64, error) {
			var id int64
			err := db.QueryRow("SELECT libraryID FROM libraries WHERE type='user' LIMIT 1").Scan(&id)
			if err != nil {
				return 0, fmt.Errorf("resolve user library ID: %w", err)
			}
			return id, nil
		},
	}
}

// ForGroup selects a specific group library by its SQLite libraryID.
// The caller is expected to know the libraryID in advance (read from
// zot.Config or the groups table). Errors if the row does not exist or
// is not a group.
func ForGroup(libraryID int64) LibrarySelector {
	return LibrarySelector{
		label: fmt.Sprintf("group(%d)", libraryID),
		resolve: func(db *sql.DB) (int64, error) {
			var id int64
			err := db.QueryRow(
				"SELECT libraryID FROM libraries WHERE libraryID=? AND type='group'",
				libraryID,
			).Scan(&id)
			if err == sql.ErrNoRows {
				return 0, fmt.Errorf("group libraryID %d not found (is it a user library or does it exist?)", libraryID)
			}
			if err != nil {
				return 0, fmt.Errorf("resolve group libraryID %d: %w", libraryID, err)
			}
			return id, nil
		},
	}
}

// ForGroupByAPIID selects a group library by its Zotero Web API groupID.
// Bridges the gap between zot.Config.SharedGroupID (API identity) and the
// SQLite libraryID used by every local query. Errors if no group row with
// that API ID is present.
func ForGroupByAPIID(apiGroupID int64) LibrarySelector {
	return LibrarySelector{
		label: fmt.Sprintf("group(api=%d)", apiGroupID),
		resolve: func(db *sql.DB) (int64, error) {
			var libID int64
			err := db.QueryRow(
				"SELECT l.libraryID FROM libraries l "+
					"JOIN groups g ON g.libraryID = l.libraryID "+
					"WHERE l.type='group' AND g.groupID=?",
				apiGroupID,
			).Scan(&libID)
			if err == sql.ErrNoRows {
				return 0, fmt.Errorf("no local group with API groupID %d — run Zotero desktop to sync, then retry", apiGroupID)
			}
			if err != nil {
				return 0, fmt.Errorf("resolve group by API ID %d: %w", apiGroupID, err)
			}
			return libID, nil
		},
	}
}
