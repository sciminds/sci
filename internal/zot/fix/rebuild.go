package fix

import (
	"fmt"

	"github.com/sciminds/sci/internal/zot/client"
)

// rewriteIfUnchanged builds the api.ItemPatch.Rebuild hook for a single-field
// rewrite that was planned against a value read from the local mirror.
//
// Both fixes in this package are plans, not derivations: the new DOI / citekey
// is a function of what the field held *at plan time*. So when the write comes
// back 412, re-deriving is not possible — the correct answer depends on why the
// server's value moved, which only a human knows. The hook therefore verifies
// the premise instead of rebuilding under it, and refuses when it no longer
// holds. Resubmitting would silently overwrite whatever landed in the meantime,
// which on a Better BibTeX–managed library is precisely the key the user cares
// about.
//
// Finding the fix already applied is success, not a conflict: that is what a
// resumed run after a partial failure looks like.
func rewriteIfUnchanged(
	field, oldVal, newVal string,
	read func(*client.Item) *string,
	write func(*string) client.ItemData,
) func(*client.Item) (client.ItemData, error) {
	return func(cur *client.Item) (client.ItemData, error) {
		server := ""
		if v := read(cur); v != nil {
			server = *v
		}
		if server != oldVal && server != newVal {
			return client.ItemData{}, fmt.Errorf(
				"%s changed on the server since the plan was built (now %q, planned against %q) — re-run the plan",
				field, server, oldVal)
		}
		v := newVal
		return write(&v), nil
	}
}
