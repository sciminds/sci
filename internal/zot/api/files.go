package api

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
)

// Attachment upload — Zotero's 4-phase file-upload dance.
//
//  1. POST /items — create the `imported_file` child attachment item
//     ([Client.CreateChildAttachment]).
//  2. POST /items/{key}/file with a form body (md5, filename, filesize,
//     mtime) — request upload authorization, or short-circuit when the
//     server already holds a file with that md5.
//  3. POST the pre-signed URL from phase 2 — stream the bytes to S3.
//  4. POST /items/{key}/file with `upload=<uploadKey>` — register the upload
//     so Zotero publishes the attachment.
//
// Phases 2 and 4 are ordinary authenticated Zotero calls. Phase 3 is not:
// the pre-signed S3 policy rejects a request carrying the Zotero-API-Key
// header, so it goes through s3Client, a plain http.Client that never sees
// the retry/auth layer.

// s3Client sends the phase-3 POST. The 10-minute timeout is belt-and-braces
// against a caller that forgets a ctx deadline: a stalled S3 endpoint must
// not hang the process indefinitely, and PDFs are a few MB in practice, so
// ten minutes sits comfortably above any legitimate upload.
var s3Client HTTPDoer = &http.Client{Timeout: 10 * time.Minute}

// now is the clock the upload stamps its mtime from; tests pin it.
var now = time.Now

// AttachmentMeta describes a new `imported_file` attachment item before its
// bytes have been uploaded. Title is optional; when empty Zotero derives one
// from Filename at display time.
type AttachmentMeta struct {
	Filename    string
	ContentType string
	Title       string
}

// CreateChildAttachment is phase 1 of an upload: it creates an
// `imported_file` attachment item under parentKey and returns it as the
// server materialised it, so the key is ready for [Client.UploadAttachmentFile]
// without a follow-up GET. Top-level attachments are the desktop drag-drop
// shape and are not offered here.
func (c *Client) CreateChildAttachment(ctx context.Context, parentKey string, meta AttachmentMeta) (*Object, error) {
	data := map[string]any{
		"itemType":    "attachment",
		"linkMode":    "imported_file",
		"parentItem":  parentKey,
		"filename":    meta.Filename,
		"contentType": meta.ContentType,
	}
	if meta.Title != "" {
		data["title"] = meta.Title
	}
	return c.CreateItem(ctx, data)
}

// uploadAuthorization is the phase-2 response when the server wants the
// bytes: where to POST them, how to frame them, and the key to hand back in
// phase 4. `params` is also present in the wire form but unused — see
// uploadToS3 for why the prefix/suffix framing is the only safe one.
type uploadAuthorization struct {
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	UploadKey   string `json:"uploadKey"`
}

// UploadAttachmentFile streams r to Zotero as the file for an attachment
// item created by [Client.CreateChildAttachment], running phases 2, 3 and 4.
// It returns uploaded=false when the server already held a file with the
// same md5 (Zotero's dedup short-circuit at phase 2), in which case nothing
// was sent to S3 and the attachment is already published.
//
// r is read to EOF into memory: the bytes are hashed once for phase 2 and
// re-sent on retry, and PDFs are a few MB in practice.
func (c *Client) UploadAttachmentFile(ctx context.Context, itemKey string, r io.Reader, filename, contentType string) (uploaded bool, err error) {
	content, err := io.ReadAll(r)
	if err != nil {
		return false, fmt.Errorf("read attachment body: %w", err)
	}
	auth, err := c.requestUploadAuth(ctx, itemKey, content, filename)
	if err != nil {
		return false, err
	}
	if auth == nil {
		return false, nil
	}
	if err := uploadToS3(ctx, auth, content); err != nil {
		return false, err
	}
	if err := c.registerUpload(ctx, itemKey, auth.UploadKey); err != nil {
		return false, err
	}
	return true, nil
}

// filePath is the per-item file endpoint phases 2 and 4 POST to.
func (c *Client) filePath(itemKey string) string {
	return c.itemPath(itemKey) + "/file"
}

// ifNoneMatchNew is the precondition for uploading a file to an attachment
// that has none yet. Replacing an existing file would send If-Match with
// the old md5 instead; sci only ever creates fresh attachments.
var ifNoneMatchNew = map[string]string{"If-None-Match": "*"}

// requestUploadAuth is phase 2. It returns the authorization when the
// server wants the bytes, or (nil, nil) when the server already holds a
// file with this md5 and phases 3 and 4 are to be skipped.
func (c *Client) requestUploadAuth(ctx context.Context, itemKey string, content []byte, filename string) (*uploadAuthorization, error) {
	sum := md5.Sum(content)
	form := url.Values{
		"md5":      {hex.EncodeToString(sum[:])},
		"filename": {filename},
		"filesize": {strconv.Itoa(len(content))},
		"mtime":    {strconv.FormatInt(now().UnixMilli(), 10)},
	}
	path := c.filePath(itemKey)
	resp, err := c.doForm(ctx, path, ifNoneMatchNew, form)
	if err != nil {
		return nil, err
	}
	if resp.Status == http.StatusPreconditionFailed {
		return nil, fmt.Errorf("upload authorization refused, the attachment already has a file: %w",
			resp.httpError(http.MethodPost, path))
	}
	if !resp.ok() {
		return nil, resp.httpError(http.MethodPost, path)
	}
	// The response is one of two shapes with no discriminator: peek at
	// `exists` before decoding the authorization.
	var peek struct {
		Exists *int `json:"exists"`
	}
	if err := resp.decode(http.MethodPost, path, &peek); err != nil {
		return nil, err
	}
	if peek.Exists != nil {
		return nil, nil
	}
	var auth uploadAuthorization
	if err := resp.decode(http.MethodPost, path, &auth); err != nil {
		return nil, err
	}
	if auth.URL == "" || auth.UploadKey == "" {
		return nil, fmt.Errorf("POST %s: authorization carries no url/uploadKey: %s", path, bytes.TrimSpace(resp.Body))
	}
	return &auth, nil
}

// registerUpload is phase 4: echo the uploadKey so Zotero publishes the
// file it now holds. Success is 204 No Content.
func (c *Client) registerUpload(ctx context.Context, itemKey, uploadKey string) error {
	path := c.filePath(itemKey)
	resp, err := c.doForm(ctx, path, ifNoneMatchNew, url.Values{"upload": {uploadKey}})
	if err != nil {
		return err
	}
	if !resp.ok() {
		return fmt.Errorf("register upload: %w", resp.httpError(http.MethodPost, path))
	}
	return nil
}

// uploadToS3 is phase 3: POST the bytes to the pre-signed URL. The body is
// exactly prefix + bytes + suffix and the Content-Type is the one the
// authorization named, both verbatim.
//
// Why not build a multipart body from the authorization's `params`:
// Zotero bakes the whole multipart preamble — boundary, every policy field
// (key, policy, signature, acl), and the file part's Content-Disposition —
// into Prefix, with Suffix as the closing boundary. `params` is an
// informational parallel listing and is sometimes empty, in which case a
// self-built form arrives with key="" and S3 answers "400 InvalidArgument:
// Bucket POST must contain a field named 'key'". Prefix/suffix works for
// both shapes.
//
// The request carries no Zotero headers: the pre-signed policy is the
// credential, and an unexpected header fails the signature check.
func uploadToS3(ctx context.Context, auth *uploadAuthorization, content []byte) error {
	body := slices.Concat([]byte(auth.Prefix), content, []byte(auth.Suffix))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("s3 post: %w", err)
	}
	if auth.ContentType != "" {
		req.Header.Set("Content-Type", auth.ContentType)
	}
	req.ContentLength = int64(len(body))

	raw, err := s3Client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 post: %w", err)
	}
	resp, err := readResponse(raw)
	if err != nil {
		return fmt.Errorf("s3 post: %w", err)
	}
	if !resp.ok() {
		return fmt.Errorf("s3 post %s: HTTP %d: %s", auth.URL, resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

// doForm is the form-encoded sibling of [Client.do]: the same Zotero headers
// and retry layer, with a application/x-www-form-urlencoded body — the
// shape the /file endpoint takes. Always a POST; that is the only method
// the endpoint answers.
func (c *Client) doForm(ctx context.Context, path string, extraHeaders map[string]string, form url.Values) (*response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	req.Header.Set("Zotero-API-Key", c.creds.APIKey)
	req.Header.Set("Zotero-API-Version", apiVersion)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.send(req, []byte(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	return resp, nil
}

// HasAttachmentWithMD5 reports whether any child attachment of parentKey
// already carries the given md5 — the check a caller makes before creating
// a second attachment for bytes the item already holds. The comparison is
// case-insensitive; Zotero reports the digest in lowercase hex.
func (c *Client) HasAttachmentWithMD5(ctx context.Context, parentKey, md5sum string) (bool, error) {
	children, err := c.ListChildren(ctx, parentKey)
	if err != nil {
		return false, err
	}
	return lo.ContainsBy(children, func(child Object) bool {
		got, _ := child.Data["md5"].(string)
		return got != "" && strings.EqualFold(got, md5sum)
	}), nil
}
