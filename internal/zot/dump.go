package zot

// dump.go — the item-plane mirror. `zot export --format ndjson` serializes
// the whole library as newline-delimited JSON so a downstream tool can
// build its own store from it without opening zotero.sqlite or holding a
// Zotero API key.
//
// This is a MIRROR, not a bibliography. ExportLibrary's citation formats
// project the library down to what a .bib needs; this keeps every field a
// consumer might join on — versions, collection membership, tags,
// attachment metadata — and leaves interpretation to the reader.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sciminds/cli/internal/zot/local"
)

// ExportNDJSON serializes the library as a kind-tagged NDJSON mirror.
// Unlike the citation formats it is consumed by machines, not manuscripts.
const ExportNDJSON ExportFormat = "ndjson"

// DumpInput is everything a dump serializes. Collections ride along with
// items because an item carries collection *keys* only — without the
// collection records a consumer can reconstruct membership but not names.
type DumpInput struct {
	// Scope is the --library value the dump was taken under
	// ("personal", "shared", or "all"). Informational: per-record
	// Library is what a consumer should trust, since under "all" there
	// is no single answer.
	Scope         string
	SchemaVersion int
	Items         []local.Item
	Collections   []local.Collection
}

// DumpStats counts what a dump emitted.
type DumpStats struct {
	Items       int `json:"items"`
	Collections int `json:"collections"`
}

// dumpRecord wraps one library object with its kind tag. The tag is what
// lets a consumer stream-decode a heterogeneous file: read a line, switch
// on kind, route the payload. Payload is inlined rather than nested so the
// record reads as a flat object with two extra fields.
type dumpRecord struct {
	Kind    string `json:"kind"`
	Library string `json:"library,omitempty"`
	payload any
}

// MarshalJSON flattens kind and library into the payload's own object.
func (r dumpRecord) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(r.payload)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["kind"], _ = json.Marshal(r.Kind)
	if r.Library != "" {
		m["library"], _ = json.Marshal(r.Library)
	}
	return json.Marshal(m)
}

// DumpNDJSON writes one JSON object per line to w and reports what it
// emitted.
//
// Collections are written before items so a streaming consumer can resolve
// an item's collection keys as it goes instead of buffering the whole file.
// Every record carries its library ("personal"/"shared"), because under
// --library all a single top-level scope would be a lie.
func DumpNDJSON(w io.Writer, in DumpInput) (DumpStats, error) {
	var stats DumpStats
	enc := json.NewEncoder(w)

	for i := range in.Collections {
		c := in.Collections[i]
		if err := enc.Encode(dumpRecord{Kind: "collection", Library: c.Library, payload: c}); err != nil {
			return stats, fmt.Errorf("encode collection %s: %w", c.Key, err)
		}
		stats.Collections++
	}
	for i := range in.Items {
		it := in.Items[i]
		if err := enc.Encode(dumpRecord{Kind: "item", Library: it.Library, payload: it}); err != nil {
			return stats, fmt.Errorf("encode item %s: %w", it.Key, err)
		}
		stats.Items++
	}
	return stats, nil
}

// DumpMeta is the sidecar written next to a dump body. It is written LAST
// and carries the body's digest, so its presence is the signal that the
// dump completed — a consumer that finds a body without a matching sidecar
// knows it caught a partial write rather than guessing.
type DumpMeta struct {
	DumpedAt      string    `json:"dumped_at"`
	ProducedBy    string    `json:"produced_by"`
	Scope         string    `json:"scope"`
	SchemaVersion int       `json:"schema_version"`
	LastSync      string    `json:"last_sync,omitempty"`
	PendingWAL    int64     `json:"pending_wal_bytes,omitempty"`
	Stats         DumpStats `json:"stats"`
	SHA256        string    `json:"sha256"`
}

// dumpMetaSuffix is appended to the body path to form the sidecar path.
const dumpMetaSuffix = ".meta.json"

// WriteDumpMeta hashes the dump body at bodyPath, stamps meta with the
// digest and the current time, and writes the sidecar beside it. Returns
// the sidecar path.
//
// It reads the body back rather than hashing in-flight on purpose: the
// digest then describes the bytes that actually landed on disk, which is
// the thing the consumer will read.
func WriteDumpMeta(bodyPath string, meta DumpMeta) (string, error) {
	f, err := os.Open(bodyPath) //nolint:gosec // path is the caller's own --out
	if err != nil {
		return "", fmt.Errorf("open dump body: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash dump body: %w", err)
	}
	meta.SHA256 = hex.EncodeToString(h.Sum(nil))
	meta.DumpedAt = time.Now().UTC().Format(time.RFC3339)
	if meta.ProducedBy == "" {
		meta.ProducedBy = "sci zot export --format ndjson"
	}

	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	metaPath := bodyPath + dumpMetaSuffix
	if err := os.WriteFile(metaPath, append(raw, '\n'), 0o644); err != nil { //nolint:gosec // sidecar mirrors the body's perms
		return "", fmt.Errorf("write dump meta: %w", err)
	}
	return metaPath, nil
}
