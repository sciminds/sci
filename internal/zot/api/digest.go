package api

import (
	"crypto/md5"
	"encoding/hex"
	"time"
)

// fileDigest carries the three facts the Zotero upload-authorization endpoint
// needs about a file payload: a hex-lowercase MD5, the byte length, and the
// modification time expressed as Unix **milliseconds** (the Zotero spec
// insists on millis, not seconds — see openapi.yaml UploadAuthRequest.mtime).
type fileDigest struct {
	MD5         string
	Size        int
	MTimeMillis int
}

// computeFileDigest hashes body with MD5 and packages it with size + mtime
// into the shape the phase-2 form body needs. MD5 is used because Zotero's
// dedup index is keyed on it — cryptographic weakness is irrelevant here.
func computeFileDigest(body []byte, mod time.Time) fileDigest {
	sum := md5.Sum(body)
	return fileDigest{
		MD5:         hex.EncodeToString(sum[:]),
		Size:        len(body),
		MTimeMillis: int(mod.UnixMilli()),
	}
}
