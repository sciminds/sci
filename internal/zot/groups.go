package zot

import (
	"strconv"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/local"
)

// LocalGroupProbe returns a [GroupProbeFunc] that lists the group libraries
// present in the Zotero database under dataDir.
//
// This is where `--library shared` gets its identity. Naming the group used
// to mean a `GET /users/{id}/groups` call, which made a Zotero API key a
// precondition for a scope that only ever reads local rows; the `groups`
// table has carried the same answer all along, works offline, and needs no
// credential.
//
// The probe opens its own short-lived handle, because it runs before a
// library scope exists — resolving the scope is what it is for.
func LocalGroupProbe(dataDir string) GroupProbeFunc {
	return func() ([]GroupRef, error) {
		db, err := local.Open(dataDir, local.ForPersonal())
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		groups, err := db.Groups()
		if err != nil {
			return nil, err
		}
		return lo.Map(groups, func(g local.Group, _ int) GroupRef {
			return GroupRef{ID: strconv.FormatInt(g.GroupID, 10), Name: g.Name}
		}), nil
	}
}
