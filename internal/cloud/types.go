// Package cloud provides a thin wrapper around the `hf buckets` CLI for
// reading and writing files in the SciMinds Hugging Face org buckets.
//
// There are two buckets under the org:
//
//   - "public"  — world-readable; files get HTTPS resolve URLs
//   - "private" — org-members-only; no public URL
//
// Objects are keyed as "<username>/<filename>" within each bucket so per-user
// listings stay scoped.
//
// Authentication is delegated entirely to `hf auth login` — sci does not
// store any tokens. [Setup] verifies the caller is logged in and a member of
// the sciminds org.
package cloud

// Bucket names within the sciminds Hugging Face org.
const (
	DefaultOrg    = "sciminds"
	BucketPublic  = "public"
	BucketPrivate = "private"
)

// Config identifies the authenticated user and target org.
type Config struct {
	Username string
	Org      string
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
	URL          string `json:"url"`
}
