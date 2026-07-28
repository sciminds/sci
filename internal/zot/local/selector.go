package local

import (
	"database/sql"
	"fmt"
	"slices"
)

// LibrarySelector chooses which row(s) in the `libraries` table a local
// DB handle pins to. It is applied once during Open — every subsequent
// query filters on the resolved libraryID(s).
//
// Callers never construct a LibrarySelector directly; use ForPersonal,
// ForGroup, ForGroupByAPIID, or ForAll. Zero value is invalid. All
// selectors but ForAll resolve to a single library; ForAll opens the
// merged read pool — see its godoc for what that mode may be used for.
type LibrarySelector struct {
	resolve func(*sql.DB) ([]int64, error)
	label   string // for error messages
}

// single adapts a one-library resolver to the multi-ID signature every
// selector now shares.
func single(f func(*sql.DB) (int64, error)) func(*sql.DB) ([]int64, error) {
	return func(db *sql.DB) ([]int64, error) {
		id, err := f(db)
		if err != nil {
			return nil, err
		}
		return []int64{id}, nil
	}
}

// ForPersonal selects the user's personal library (`libraries.type='user'`).
// There is always exactly one user library per Zotero account.
func ForPersonal() LibrarySelector {
	return LibrarySelector{
		label: "personal",
		resolve: single(func(db *sql.DB) (int64, error) {
			var id int64
			err := db.QueryRow("SELECT libraryID FROM libraries WHERE type='user' LIMIT 1").Scan(&id)
			if err != nil {
				return 0, fmt.Errorf("resolve user library ID: %w", err)
			}
			return id, nil
		}),
	}
}

// ForGroup selects a specific group library by its SQLite libraryID.
// The caller is expected to know the libraryID in advance (read from
// zot.Config or the groups table). Errors if the row does not exist or
// is not a group.
func ForGroup(libraryID int64) LibrarySelector {
	return LibrarySelector{
		label: fmt.Sprintf("group(%d)", libraryID),
		resolve: single(func(db *sql.DB) (int64, error) {
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
		}),
	}
}

// ForGroupByAPIID selects a group library by its Zotero Web API groupID.
// Bridges the gap between zot.Config.SharedGroupID (API identity) and the
// SQLite libraryID used by every local query. Errors if no group row with
// that API ID is present.
func ForGroupByAPIID(apiGroupID int64) LibrarySelector {
	return LibrarySelector{
		label: fmt.Sprintf("group(api=%d)", apiGroupID),
		resolve: single(func(db *sql.DB) (int64, error) {
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
		}),
	}
}

// ForAll selects the merged read pool: the personal library plus the
// shared group identified by its Zotero Web API groupID. Multi-library
// handles serve ONLY the converted query paths — Search, List/ListAll/
// CountList, Read, and their enrichment — and it is the CLI's job to
// gate which commands may open one (`--library all` is rejected
// elsewhere). An unsynced group is an error, not a silent degrade to
// personal-only: a merged pool that quietly lost half its libraries
// would answer a different question than the one asked.
func ForAll(apiGroupID int64) LibrarySelector {
	return LibrarySelector{
		label: fmt.Sprintf("all(personal+group api=%d)", apiGroupID),
		resolve: func(db *sql.DB) ([]int64, error) {
			personal, err := ForPersonal().resolve(db)
			if err != nil {
				return nil, err
			}
			group, err := ForGroupByAPIID(apiGroupID).resolve(db)
			if err != nil {
				return nil, err
			}
			return slices.Concat(personal, group), nil
		},
	}
}
