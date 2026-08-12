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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// attachSkipExisting backs `item attach --skip-existing`.
var attachSkipExisting bool

func itemAttachCommand() *cli.Command {
	return &cli.Command{
		Name:      "attach",
		Usage:     "Upload a local file as a child attachment of an existing item",
		ArgsUsage: "<parent-key> <path>",
		Description: "$ sci zot --library personal item attach ABC12345 ~/papers/Smith2022.pdf\n" +
			"$ sci zot --library personal item attach ABC12345 ~/papers/Smith2022.pdf --skip-existing\n" +
			"\n" +
			"Creates a new imported_file attachment as a child of <parent-key> and\n" +
			"uploads the file bytes. Existing attachments on the parent are left\n" +
			"untouched — running this twice against the same parent produces two\n" +
			"attachment items; Zotero's server-side dedup may share storage for\n" +
			"identical bytes but the attachment items are still distinct.\n" +
			"\n" +
			"Pass --skip-existing to make the command idempotent: it checks the\n" +
			"parent's children over the Web API first and no-ops when one already\n" +
			"carries the same md5. That check is what makes a batch attach safely\n" +
			"resumable — a local `item children` read cannot answer it, since the\n" +
			"mirror does not see attachments this CLI just wrote.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "skip-existing", Usage: "no-op when the parent already has an attachment with the same md5 (one extra API call)", Destination: &attachSkipExisting, Local: true},
		},
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

	// The guard is a REMOTE read on purpose. `item children` against the
	// local mirror answers zero for a parent whose attachments were written
	// since the last Zotero desktop sync — which is exactly the state a
	// resumed batch is in — and a caller that trusts that zero uploads the
	// file a second time.
	if attachSkipExisting {
		digest, err := fileMD5(path)
		if err != nil {
			return err
		}
		children, err := c.ListChildren(ctx, parentKey)
		if err != nil {
			return fmt.Errorf("check existing attachments: %w", err)
		}
		if key := findExistingAttachment(children, digest); key != "" {
			meta.close()
			outputScoped(ctx, cmd, zot.WriteResult{
				Action:  "skipped",
				Kind:    "item",
				Target:  key,
				Message: fmt.Sprintf("item %s already has %s with the same contents (md5 %s)", parentKey, key, digest),
			})
			return nil
		}
	}

	it, err := c.CreateChildAttachment(ctx, parentKey, meta.meta)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	defer meta.close()

	if err := c.UploadAttachmentFile(ctx, it.Key, meta.file, meta.meta.Filename, meta.meta.ContentType); err != nil {
		return fmt.Errorf("attachment %s created but upload failed: %w", it.Key, err)
	}

	outputScoped(ctx, cmd, zot.WriteResult{
		Action:  "added",
		Kind:    "item",
		Target:  it.Key,
		Message: fmt.Sprintf("attached %s to item %s", filepath.Base(path), parentKey),
		Data:    api.ItemFromClient(it),
	})
	return nil
}

// fileMD5 returns the hex md5 of a file's bytes — the same digest Zotero
// stores for an imported_file attachment, so the two are directly comparable.
// md5 is not a security choice here: it is the hash Zotero's API reports, and
// matching it is the whole point.
func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	h := md5.New() //nolint:gosec // content identity against Zotero's md5, not a security boundary
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// findExistingAttachment returns the key of a child attachment whose stored
// bytes match digest, or "" when the parent has none.
//
// Matching on md5 rather than filename is deliberate: the same paper saved
// under two names is one attachment, and two different papers that happen to
// share a filename are two. An empty digest on either side never matches —
// Zotero leaves md5 blank for linked URLs and for files it has not hashed
// yet, and treating unknown == unknown would skip a real upload.
func findExistingAttachment(children []api.ChildItem, digest string) string {
	if digest == "" {
		return ""
	}
	match, ok := lo.Find(children, func(ch api.ChildItem) bool {
		return ch.ItemType == "attachment" && ch.Md5 == digest
	})
	if !ok {
		return ""
	}
	return match.Key
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
