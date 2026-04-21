package pdffind

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/zot/api"
)

// Attacher is the narrow Zotero-write contract Attach depends on. Kept as an
// interface so tests can substitute an in-memory stub. *api.Client satisfies
// it via its CreateChildAttachment and UploadAttachmentFile methods.
type Attacher interface {
	CreateChildAttachment(ctx context.Context, parentKey string, meta api.AttachmentMeta) (string, error)
	UploadAttachmentFile(ctx context.Context, itemKey string, r io.Reader, filename, contentType string) error
}

// AttachOptions configures Attach. Zero value is valid — no callbacks, serial.
type AttachOptions struct {
	// OnStart fires just before each finding's create+upload cycle begins.
	// total counts only findings with a DownloadedPath (skipped items don't
	// advance i or total).
	OnStart func(i, total int, f Finding)
	// OnDone fires after create+upload completes (success OR error).
	// The Finding passed in carries the final AttachmentKey / AttachError.
	OnDone func(i, total int, f Finding)
}

// Attach uploads every finding's downloaded PDF back to Zotero as a child
// attachment of the original item. Mutates each Finding's AttachmentKey or
// AttachError in place. Returns the mutated slice.
//
// Per-item failures (create error, upload error, missing file on disk) are
// recorded on the Finding and do NOT abort the batch. Context cancellation
// DOES abort, returning ctx.Err().
//
// Findings without a DownloadedPath are passed through untouched — running
// Attach on a pre-download result set is a no-op by design, not an error.
//
// The flow is strictly serial. Zotero's Web API rate-limits aggressively and
// the two-phase (create → upload) sequence is already chatty per item; going
// parallel multiplies the 429-risk without meaningfully shortening wall time.
func Attach(ctx context.Context, w Attacher, findings []Finding, opts AttachOptions) ([]Finding, error) {
	total := lo.CountBy(findings, func(f Finding) bool { return f.DownloadedPath != "" })

	idx := 0
	for i := range findings {
		if findings[i].DownloadedPath == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		if opts.OnStart != nil {
			opts.OnStart(idx, total, findings[i])
		}
		attachOne(ctx, w, &findings[i])
		if opts.OnDone != nil {
			opts.OnDone(idx, total, findings[i])
		}
		idx++
	}
	return findings, nil
}

// attachOne runs phase-1 (CreateChildAttachment) and phases 2-4
// (UploadAttachmentFile) for a single finding, recording outcomes on f.
//
// When create succeeds but upload fails, we deliberately leave AttachmentKey
// populated — the attachment item exists on Zotero, just without bytes. The
// renderer surfaces this as "created, not uploaded" so the user knows to
// retry or clean up.
func attachOne(ctx context.Context, w Attacher, f *Finding) {
	filename := filepath.Base(f.DownloadedPath)
	meta := api.AttachmentMeta{
		Filename:    filename,
		ContentType: "application/pdf",
		Title:       f.Title,
	}
	key, err := w.CreateChildAttachment(ctx, f.ItemKey, meta)
	if err != nil {
		f.AttachError = fmt.Sprintf("create attachment: %s", err.Error())
		return
	}
	f.AttachmentKey = key

	file, err := os.Open(f.DownloadedPath)
	if err != nil {
		f.AttachError = fmt.Sprintf("open downloaded pdf: %s", err.Error())
		return
	}
	defer func() { _ = file.Close() }()

	if err := w.UploadAttachmentFile(ctx, key, file, filename, "application/pdf"); err != nil {
		f.AttachError = fmt.Sprintf("upload attachment: %s", err.Error())
		return
	}
}
