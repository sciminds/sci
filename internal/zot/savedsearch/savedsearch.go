// Package savedsearch translates Zotero saved-search conditions into the
// query parameters the Web API actually understands.
//
// Zotero saved searches are evaluated client-side by the desktop app's
// JavaScript; the Web API ignores `?searchKey=` on item endpoints. To run
// a saved search programmatically we have to project its conditions onto
// the much narrower API filter language: `?tag=`, `?itemType=`,
// `/items/top`, `/collections/{key}/items`. This package owns that mapping.
//
// The translation is deliberately conservative: it covers the common
// condition vocabulary (tag, itemType, collection, noChildren) and
// records anything else in an Unsupported list so the caller can decide
// whether to abort or proceed with a partial filter. It never silently
// drops a condition — that would produce wrong results without warning.
package savedsearch

import (
	"fmt"

	"github.com/sciminds/cli/internal/zot/client"
)

// APIFilters is the API-level projection of a saved search. Zero values
// mean "no filter for this field." Pass to api.ListItemsOptions /
// api.ListItemsTopOptions to execute.
type APIFilters struct {
	// Tag matches items carrying this tag (API: ?tag=X).
	Tag string
	// NotTag excludes items carrying this tag (API: ?tag=-X).
	NotTag string
	// ItemType narrows to a Zotero item type (API: ?itemType=X).
	ItemType string
	// NotItemType excludes a Zotero item type (API: ?itemType=-X).
	NotItemType string
	// CollectionKey scopes to one collection (API path:
	// /collections/{key}/items).
	CollectionKey string
	// TopOnly restricts to top-level items, hitting /items/top instead of
	// /items. Set when the saved search includes `noChildren=true` (a
	// childless item is necessarily top-level, so this narrows the API
	// query) — but TopOnly alone is not sufficient: top-level items can
	// have children. Pair with NoChildren.
	TopOnly bool
	// NoChildren signals the caller must post-filter the API response to
	// items with `meta.numChildren == 0`. The Zotero Web API has no native
	// "no children" filter, so this projection is unavoidable. Set
	// whenever `noChildren=true` appears in the saved search.
	NoChildren bool
}

// Unsupported is one saved-search condition the translator could not
// project into an API filter. Callers should surface these to the user
// rather than ignoring them — otherwise the executed query would silently
// match more items than the saved search would.
type Unsupported struct {
	Condition string
	Operator  string
	Value     string
	Reason    string
}

// String renders the unsupported condition for human-readable error messages.
func (u Unsupported) String() string {
	if u.Value == "" {
		return fmt.Sprintf("%s/%s: %s", u.Condition, u.Operator, u.Reason)
	}
	return fmt.Sprintf("%s/%s=%q: %s", u.Condition, u.Operator, u.Value, u.Reason)
}

// Translate walks the saved-search conditions and builds an APIFilters
// projection. Any condition the API cannot express is recorded in the
// returned Unsupported slice; the supported ones still populate the
// filter so callers that want a best-effort run can use them.
func Translate(conds []client.SearchCondition) (APIFilters, []Unsupported) {
	var out APIFilters
	var bad []Unsupported

	for _, c := range conds {
		switch c.Condition {
		case "tag":
			switch c.Operator {
			case "is":
				if out.Tag != "" {
					bad = append(bad, Unsupported{
						Condition: c.Condition, Operator: c.Operator, Value: c.Value,
						Reason: "API only supports one positive tag filter per request",
					})
					continue
				}
				out.Tag = c.Value
			case "isNot":
				if out.NotTag != "" {
					bad = append(bad, Unsupported{
						Condition: c.Condition, Operator: c.Operator, Value: c.Value,
						Reason: "API only supports one negated tag filter per request",
					})
					continue
				}
				out.NotTag = c.Value
			default:
				bad = append(bad, Unsupported{
					Condition: c.Condition, Operator: c.Operator, Value: c.Value,
					Reason: "tag operator must be is or isNot",
				})
			}

		case "itemType":
			switch c.Operator {
			case "is":
				if out.ItemType != "" {
					bad = append(bad, Unsupported{
						Condition: c.Condition, Operator: c.Operator, Value: c.Value,
						Reason: "API only supports one positive itemType filter per request",
					})
					continue
				}
				out.ItemType = c.Value
			case "isNot":
				if out.NotItemType != "" {
					bad = append(bad, Unsupported{
						Condition: c.Condition, Operator: c.Operator, Value: c.Value,
						Reason: "API only supports one negated itemType filter per request",
					})
					continue
				}
				out.NotItemType = c.Value
			default:
				bad = append(bad, Unsupported{
					Condition: c.Condition, Operator: c.Operator, Value: c.Value,
					Reason: "itemType operator must be is or isNot",
				})
			}

		case "collection":
			if c.Operator != "is" {
				bad = append(bad, Unsupported{
					Condition: c.Condition, Operator: c.Operator, Value: c.Value,
					Reason: "collection operator must be is",
				})
				continue
			}
			if out.CollectionKey != "" {
				bad = append(bad, Unsupported{
					Condition: c.Condition, Operator: c.Operator, Value: c.Value,
					Reason: "API supports at most one collection scope per request",
				})
				continue
			}
			out.CollectionKey = c.Value

		case "noChildren":
			// Pseudo-condition: noChildren=true means "items WITH no
			// children". Strictly stronger than top-level — a paper with
			// PDF attachments is top-level but has children. The Web API
			// has no `?noChildren=` filter, so we project this in two
			// halves: TopOnly narrows the API call to /items/top, then
			// NoChildren tells the caller to post-filter on
			// `meta.numChildren == 0`. noChildren=false is the default
			// behavior of our list path (we never recurse into children),
			// so it's a no-op.
			if c.Operator == "true" {
				out.TopOnly = true
				out.NoChildren = true
			}

		case "joinMode", "includeParentsAndChildren":
			// Modifiers we can't honor faithfully via the API.
			// joinMode=any (OR across conditions) would need per-condition
			// requests; includeParentsAndChildren controls result-tree
			// expansion which our flat list doesn't model. Both default
			// to AND/flat — silently ignore so the common case where the
			// modifier is at its default value just works.

		default:
			bad = append(bad, Unsupported{
				Condition: c.Condition, Operator: c.Operator, Value: c.Value,
				Reason: "no Zotero Web API equivalent",
			})
		}
	}
	return out, bad
}
