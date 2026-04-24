package cli

// `zot item attach <parentKey> <path>` uploads a local file as a NEW child
// attachment of an existing parent item. Does not touch existing attachments
// on the parent — each invocation adds a fresh `imported_file` attachment.
// PDF bytes are streamed verbatim (no filtering, no re-encoding), so
// annotations and metadata inside the file round-trip to Zotero intact.
//
// Standalone (top-level) attachments are not exposed via the Web API surface —
// uploading a bare PDF without a parent item is `zot import`'s job (it routes
// through Zotero desktop, which runs CrossRef/arXiv metadata recognition and
// produces a proper bib item). The Web API does no such enrichment, so a
// parent-less Web API attachment is always the wrong call.

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/urfave/cli/v3"
)

func itemAttachCommand() *cli.Command {
	return &cli.Command{
		Name:      "attach",
		Usage:     "Upload a local file as a child attachment of an existing item",
		ArgsUsage: "<parent-key> <path>",
		Description: "$ zot --library personal item attach ABC12345 ~/papers/Smith2022.pdf\n" +
			"\n" +
			"Creates a new imported_file attachment as a child of <parent-key> and\n" +
			"uploads the file bytes. Existing attachments on the parent are left\n" +
			"untouched — running this twice against the same parent produces two\n" +
			"attachment items; Zotero's server-side dedup may share storage for\n" +
			"identical bytes but the attachment items are still distinct.",
		Action: runItemAttach,
	}
}

func runItemAttach(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 2 {
		return cmdutil.UsageErrorf(cmd, "expected <parent-key> <path>")
	}
	parentKey, path := args[0], args[1]
	return attachFileToParent(ctx, cmd, parentKey, path)
}

// attachFileToParent runs the create + upload pair for a single file under an
// existing parent. Shared between `item attach` and any future bulk paths.
// Reports the attachment key on a partial failure (create OK, upload failed)
// so the user can retry the upload or clean up the orphan attachment.
func attachFileToParent(ctx context.Context, cmd *cli.Command, parentKey, path string) error {
	meta, err := openAttachmentSource(path)
	if err != nil {
		return err
	}

	c, err := requireAPIClient(ctx)
	if err != nil {
		return err
	}
	it, err := c.CreateChildAttachment(ctx, parentKey, meta.meta)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	defer meta.close()

	if err := c.UploadAttachmentFile(ctx, it.Key, meta.file, meta.meta.Filename, meta.meta.ContentType); err != nil {
		return fmt.Errorf("attachment %s created but upload failed: %w", it.Key, err)
	}

	cmdutil.Output(cmd, zot.WriteResult{
		Action:  "added",
		Kind:    "item",
		Target:  it.Key,
		Message: fmt.Sprintf("attached %s to item %s", filepath.Base(path), parentKey),
		Data:    api.ItemFromClient(it),
	})
	return nil
}

// attachmentSource bundles the open file handle, its metadata, and a closer —
// so runAddFile / attachFileToParent don't each need their own os.Open ritual.
type attachmentSource struct {
	file  *os.File
	meta  api.AttachmentMeta
	close func()
}

// openAttachmentSource opens `path` for reading and derives AttachmentMeta
// (filename, content type) from the path. Caller MUST call src.close() when
// the upload is complete (or failed) to release the file handle.
func openAttachmentSource(path string) (*attachmentSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	return &attachmentSource{
		file:  f,
		meta:  buildAttachmentMetaFromPath(path),
		close: func() { _ = f.Close() },
	}, nil
}

// buildAttachmentMetaFromPath derives Zotero upload metadata from a filesystem
// path. Filename is the basename; ContentType comes from mime.TypeByExtension
// with fallbacks: .pdf → application/pdf (TypeByExtension can be empty on
// minimal systems), other unknown → application/octet-stream. Title is left
// empty — Zotero displays the filename in the UI when Title is absent, which
// matches Zotero desktop's drag-drop behavior.
func buildAttachmentMetaFromPath(path string) api.AttachmentMeta {
	ext := filepath.Ext(path)
	ct := mime.TypeByExtension(ext)
	switch {
	case ct != "":
		// stdlib had a registered mapping — use it.
	case strings.EqualFold(ext, ".pdf"):
		ct = "application/pdf"
	default:
		ct = "application/octet-stream"
	}
	return api.AttachmentMeta{
		Filename:    filepath.Base(path),
		ContentType: ct,
	}
}
