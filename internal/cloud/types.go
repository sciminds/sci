// Package cloud provides an S3-compatible client for Cloudflare R2 object
// storage. It replaces the previous PocketBase backend with direct R2 access.
//
// Credentials (account ID, access key, secret, username) are stored locally
// at [DefaultConfigPath] and managed by [LoadConfig] / [SaveConfig].
//
// Objects are keyed as "<username>/<filename>" within the configured bucket.
package cloud

// BucketConfig holds credentials for a single R2 bucket.
type BucketConfig struct {
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key"`
	BucketName string `json:"bucket_name"`
	PublicURL  string `json:"public_url,omitempty"`
}

// Config holds R2 credentials and user identity.
type Config struct {
	Username    string        `json:"username"`
	GitHubLogin string        `json:"github_login,omitempty"`
	AccountID   string        `json:"account_id"`
	Public      *BucketConfig `json:"public,omitempty"`

	// Legacy flat fields — populated only when reading old-format files.
	// Deprecated: run "sci cloud setup" to migrate to the new format.
	LegacyAccessKey  string `json:"access_key,omitempty"`
	LegacySecretKey  string `json:"secret_key,omitempty"`
	LegacyPublicURL  string `json:"public_url,omitempty"`
	LegacyBucketName string `json:"bucket_name,omitempty"`
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
	URL          string `json:"url"`
	Description  string `json:"description,omitempty"`
}
