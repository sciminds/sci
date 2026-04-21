// Package api — file upload flow (4-phase dance).
//
// Zotero's attachment upload uses a 4-call sequence:
//  1. POST /items — create the `imported_file` child attachment item (done
//     elsewhere: CreateChildAttachment reuses CreateItem).
//  2. POST /items/{key}/file (form md5/filename/filesize/mtime) — request
//     upload authorization OR short-circuit if the file is already on the
//     server (dedup).
//  3. POST auth.URL (multipart; NOT modeled in OpenAPI) — stream the file
//     bytes to S3 with the pre-signed params.
//  4. POST /items/{key}/file (form upload=<uploadKey>) — register the
//     upload so Zotero publishes the attachment.
//
// We bypass the oapi-codegen-generated `UploadFileFormdataRequestBody` for
// phases 2 and 4 because its body is encoded as a `union json.RawMessage`
// wrapping the `oneOf` schema — unusable without unsafe reflection. Instead
// we hand-encode the form bodies and call `UploadFileWithBody` directly.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/sciminds/cli/internal/zot/client"
)

// AttachmentMeta describes a new `imported_file` attachment item before its
// bytes have been uploaded. Title is optional; when empty Zotero derives one
// from Filename at display time.
type AttachmentMeta struct {
	Filename    string
	ContentType string
	Title       string
}

// CreateChildAttachment performs phase 1 of the upload dance: it creates an
// `imported_file` attachment item as a child of parentKey. The returned key
// is the Zotero item key to pass to UploadAttachmentFile for phases 2→4.
func (c *Client) CreateChildAttachment(ctx context.Context, parentKey string, meta AttachmentMeta) (string, error) {
	parent := parentKey
	filename := meta.Filename
	ctype := meta.ContentType
	title := meta.Title
	linkMode := client.ImportedFile

	data := client.ItemData{
		ItemType:    client.Attachment,
		LinkMode:    &linkMode,
		ParentItem:  &parent,
		Filename:    &filename,
		ContentType: &ctype,
	}
	if title != "" {
		data.Title = &title
	}
	return c.CreateItem(ctx, data)
}

// UploadAuthorization is the phase-2 authorization object: everything needed
// to stream the file to S3 in phase 3, plus the key we'll hand back to Zotero
// in phase 4.
type UploadAuthorization struct {
	URL         string
	ContentType string
	Prefix      string
	Suffix      string
	UploadKey   string
	Params      map[string]string
}

// errUploadExists is the phase-2 sentinel indicating Zotero's dedup store
// already has a file with the submitted MD5 — skip phases 3 and 4. Callers
// compare with errors.Is.
var errUploadExists = errors.New("zotero: file already exists for this item (dedup hit)")

// uploadFileFormBody dispatches the phase-2/phase-4 POST to the right generated
// helper (user vs. group scope) with the hand-encoded form body. Returning raw
// (status, body, err) keeps the two phases' response-decoding logic in their
// own functions where it's easier to read.
func (c *Client) uploadFileFormBody(ctx context.Context, itemKey string, form url.Values) (int, []byte, error) {
	body := strings.NewReader(form.Encode())
	ctype := "application/x-www-form-urlencoded"
	star := client.UploadFileParamsIfNoneMatchAsterisk
	starGroup := client.UploadFileGroupParamsIfNoneMatchAsterisk

	if c.isShared() {
		params := &client.UploadFileGroupParams{IfNoneMatch: &starGroup}
		r, err := c.Gen.UploadFileGroupWithBodyWithResponse(ctx, c.GroupID(), client.ItemKeyPath(itemKey), params, ctype, body)
		if err != nil {
			return 0, nil, err
		}
		return r.StatusCode(), r.Body, nil
	}
	params := &client.UploadFileParams{IfNoneMatch: &star}
	r, err := c.Gen.UploadFileWithBodyWithResponse(ctx, c.UserID, client.ItemKeyPath(itemKey), params, ctype, body)
	if err != nil {
		return 0, nil, err
	}
	return r.StatusCode(), r.Body, nil
}

// requestUploadAuth performs phase 2 of the upload dance. Returns either a
// usable UploadAuthorization OR errUploadExists (file already on server,
// phases 3 & 4 should be skipped). Any other outcome is a hard error.
func (c *Client) requestUploadAuth(ctx context.Context, itemKey string, d fileDigest, filename string) (*UploadAuthorization, error) {
	form := url.Values{
		"md5":      {d.MD5},
		"filename": {filename},
		"filesize": {strconv.Itoa(d.Size)},
		"mtime":    {strconv.Itoa(d.MTimeMillis)},
	}
	status, body, err := c.uploadFileFormBody(ctx, itemKey, form)
	if err != nil {
		return nil, err
	}
	if status == http.StatusPreconditionFailed {
		return nil, fmt.Errorf("POST /items/%s/file: 412 Precondition Failed (md5/if-match mismatch)", itemKey)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("POST /items/%s/file: status %d: %s", itemKey, status, string(body))
	}

	// The oneOf response has no discriminator — peek at `exists` first. A
	// non-nil Exists means the dedup short-circuit fired; otherwise decode
	// the full authorization object.
	var peek struct {
		Exists *int `json:"exists"`
	}
	if err := json.Unmarshal(body, &peek); err != nil {
		return nil, fmt.Errorf("decode upload auth response: %w", err)
	}
	if peek.Exists != nil {
		return nil, errUploadExists
	}

	var raw client.UploadAuth
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode upload auth response: %w", err)
	}
	// client.UploadAuth.Params is `*map[string]interface{}` (oapi-codegen's
	// rendering of the spec's `additionalProperties: true`). Normalize to
	// plain string→string, which is all S3's POST form actually accepts.
	params := map[string]string{}
	if raw.Params != nil {
		for k, v := range *raw.Params {
			params[k] = fmt.Sprint(v)
		}
	}
	return &UploadAuthorization{
		URL:         raw.Url,
		ContentType: raw.ContentType,
		Prefix:      raw.Prefix,
		Suffix:      raw.Suffix,
		UploadKey:   raw.UploadKey,
		Params:      params,
	}, nil
}

// UploadAttachmentFile streams `r` to Zotero as the file bytes for an
// already-created attachment item (see CreateChildAttachment for the phase-1
// item creation). It orchestrates phases 2→3→4 with a dedup short-circuit
// when the server already has a file with the computed MD5.
//
// r is read to EOF into memory. PDFs in practice are a few MB; buffering
// keeps the implementation simple and lets us hash once. Very large
// attachments (multi-GB) would warrant a streaming rewrite — not a concern
// for the Zotero PDF use case today.
func (c *Client) UploadAttachmentFile(ctx context.Context, itemKey string, r io.Reader, filename, contentType string) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read attachment body: %w", err)
	}
	digest := computeFileDigest(body, c.now())

	auth, err := c.requestUploadAuth(ctx, itemKey, digest, filename)
	if err != nil {
		if errors.Is(err, errUploadExists) {
			return nil // dedup hit — attachment is already published
		}
		return err
	}

	// Plain http client: the S3 URL is external and must NOT carry Zotero
	// auth headers (the retryDoer would inject them). Default client is
	// fine — tests use httptest.Server URLs that route through it.
	if err := uploadToS3(ctx, http.DefaultClient, auth, body); err != nil {
		return err
	}
	return c.registerUpload(ctx, itemKey, auth.UploadKey)
}

// registerUpload performs phase 4: tell Zotero the S3 upload completed by
// echoing the uploadKey from phase 2. Success is HTTP 204 No Content.
func (c *Client) registerUpload(ctx context.Context, itemKey, uploadKey string) error {
	form := url.Values{"upload": {uploadKey}}
	status, body, err := c.uploadFileFormBody(ctx, itemKey, form)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusPreconditionFailed:
		return fmt.Errorf("POST /items/%s/file register: 412 Precondition Failed", itemKey)
	default:
		return fmt.Errorf("POST /items/%s/file register: status %d: %s", itemKey, status, string(body))
	}
}

// uploadToS3 performs phase 3: a plain multipart/form-data POST to the
// pre-signed S3 URL carried in auth.URL. The caller supplies the raw file
// bytes; we wrap them with auth.Prefix/Suffix inside the "file" form field
// (S3's policy pins the byte layout, so this wrapping is non-optional).
//
// httpClient is explicit so the orchestrator can share a client with the
// phase 2/4 path, and tests can substitute an httptest.Server's client.
func uploadToS3(ctx context.Context, httpClient *http.Client, auth *UploadAuthorization, fileBytes []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// Deterministic field ordering: S3 doesn't care, but it keeps error
	// bodies diff-friendly when debugging 403s.
	for _, k := range slices.Sorted(maps.Keys(auth.Params)) {
		if err := w.WriteField(k, auth.Params[k]); err != nil {
			return fmt.Errorf("write form field %q: %w", k, err)
		}
	}

	// The "file" field's body is prefix + fileBytes + suffix. The part's
	// Content-Type MUST match the contentType the auth response returned —
	// S3's pre-signed policy is keyed on it.
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"`)
	if auth.ContentType != "" {
		header.Set("Content-Type", auth.ContentType)
	}
	part, err := w.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create file part: %w", err)
	}
	if _, err := io.WriteString(part, auth.Prefix); err != nil {
		return err
	}
	if _, err := part.Write(fileBytes); err != nil {
		return err
	}
	if _, err := io.WriteString(part, auth.Suffix); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.URL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("s3 post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("s3 post %s: %d: %s", auth.URL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
