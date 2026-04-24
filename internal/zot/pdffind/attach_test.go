package pdffind

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sciminds/cli/internal/zot/api"
	"github.com/sciminds/cli/internal/zot/client"
)

// fakeAttacher records every call and scripts per-parent outcomes. One per
// parentItemKey: the first Create response maps to an attachment key, and the
// first Upload call may inject an error.
type fakeAttacher struct {
	// scripting
	createKeyByParent map[string]string // parent → new attachment key
	createErrByParent map[string]error
	uploadErrByKey    map[string]error

	// observations
	creates   []api.AttachmentMeta // in order
	uploads   []string             // attachment keys uploaded, in order
	uploaded  map[string][]byte    // key → bytes received
	filenames map[string]string    // key → filename passed to UploadAttachmentFile
	parents   []string             // parent key per create
}

func newFakeAttacher() *fakeAttacher {
	return &fakeAttacher{
		createKeyByParent: map[string]string{},
		createErrByParent: map[string]error{},
		uploadErrByKey:    map[string]error{},
		uploaded:          map[string][]byte{},
		filenames:         map[string]string{},
	}
}

func (f *fakeAttacher) CreateChildAttachment(_ context.Context, parentKey string, meta api.AttachmentMeta) (*client.Item, error) {
	f.creates = append(f.creates, meta)
	f.parents = append(f.parents, parentKey)
	if err, ok := f.createErrByParent[parentKey]; ok {
		return nil, err
	}
	return &client.Item{Key: f.createKeyByParent[parentKey]}, nil
}

func (f *fakeAttacher) UploadAttachmentFile(_ context.Context, itemKey string, r io.Reader, filename, _ string) error {
	f.uploads = append(f.uploads, itemKey)
	f.filenames[itemKey] = filename
	body, _ := io.ReadAll(r)
	f.uploaded[itemKey] = body
	if err, ok := f.uploadErrByKey[itemKey]; ok {
		return err
	}
	return nil
}

// writePDF stashes a tiny "PDF" on disk and returns its path.
func writePDF(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAttach_CreatesAndUploadsEveryDownloadedFinding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := writePDF(t, dir, "ABC.pdf", []byte("%PDF-A"))
	pathB := writePDF(t, dir, "DEF.pdf", []byte("%PDF-B"))

	findings := []Finding{
		{ItemKey: "ABC", Title: "Paper A", DownloadedPath: pathA},
		{ItemKey: "DEF", Title: "Paper B", DownloadedPath: pathB},
	}

	fa := newFakeAttacher()
	fa.createKeyByParent = map[string]string{"ABC": "ATTACHA1", "DEF": "ATTACHB1"}

	out, err := Attach(context.Background(), fa, findings, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].AttachmentKey != "ATTACHA1" {
		t.Errorf("ABC attachment key = %q, want ATTACHA1", out[0].AttachmentKey)
	}
	if out[1].AttachmentKey != "ATTACHB1" {
		t.Errorf("DEF attachment key = %q, want ATTACHB1", out[1].AttachmentKey)
	}
	if len(fa.creates) != 2 || len(fa.uploads) != 2 {
		t.Fatalf("calls: creates=%d uploads=%d, want 2/2", len(fa.creates), len(fa.uploads))
	}
	// Each upload's bytes must match the corresponding downloaded file —
	// catches "used wrong finding's path" regressions.
	if string(fa.uploaded["ATTACHA1"]) != "%PDF-A" {
		t.Errorf("ATTACHA1 body = %q", fa.uploaded["ATTACHA1"])
	}
	if string(fa.uploaded["ATTACHB1"]) != "%PDF-B" {
		t.Errorf("ATTACHB1 body = %q", fa.uploaded["ATTACHB1"])
	}
	// Filename passed to the upload should be the basename, not the full path.
	if fa.filenames["ATTACHA1"] != "ABC.pdf" {
		t.Errorf("filename = %q, want ABC.pdf (basename)", fa.filenames["ATTACHA1"])
	}
}

func TestAttach_PassesTitleAndFilenameToMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writePDF(t, dir, "xyz.pdf", []byte("%PDF"))

	findings := []Finding{
		{ItemKey: "ABC", Title: "Attention is all you need", DownloadedPath: p},
	}
	fa := newFakeAttacher()
	fa.createKeyByParent = map[string]string{"ABC": "ATT1"}

	if _, err := Attach(context.Background(), fa, findings, AttachOptions{}); err != nil {
		t.Fatal(err)
	}
	if fa.creates[0].Title != "Attention is all you need" {
		t.Errorf("create meta title = %q", fa.creates[0].Title)
	}
	if fa.creates[0].Filename != "xyz.pdf" {
		t.Errorf("create meta filename = %q, want xyz.pdf", fa.creates[0].Filename)
	}
	if fa.creates[0].ContentType != "application/pdf" {
		t.Errorf("create meta content-type = %q, want application/pdf", fa.creates[0].ContentType)
	}
}

func TestAttach_SkipsFindingsWithoutDownloadedPath(t *testing.T) {
	t.Parallel()
	findings := []Finding{
		{ItemKey: "ABC"}, // nothing to attach
		{ItemKey: "DEF", DownloadError: "http 403"}, // download failed
		{ItemKey: "GHI", LookupError: "not found"},  // lookup failed
	}
	fa := newFakeAttacher()

	out, err := Attach(context.Background(), fa, findings, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fa.creates) != 0 || len(fa.uploads) != 0 {
		t.Errorf("no API calls should be made; creates=%d uploads=%d", len(fa.creates), len(fa.uploads))
	}
	for _, f := range out {
		if f.AttachmentKey != "" || f.AttachError != "" {
			t.Errorf("finding %s was touched but has no downloaded path: %+v", f.ItemKey, f)
		}
	}
}

func TestAttach_RecordsCreateErrorAndContinues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := writePDF(t, dir, "A.pdf", []byte("%PDF-A"))
	pathB := writePDF(t, dir, "B.pdf", []byte("%PDF-B"))

	findings := []Finding{
		{ItemKey: "BAD", DownloadedPath: pathA},
		{ItemKey: "OK", DownloadedPath: pathB},
	}
	fa := newFakeAttacher()
	fa.createErrByParent = map[string]error{"BAD": errors.New("boom on create")}
	fa.createKeyByParent = map[string]string{"OK": "ATTACHOK"}

	out, err := Attach(context.Background(), fa, findings, AttachOptions{})
	if err != nil {
		t.Fatalf("batch must not abort on per-item failure: %v", err)
	}
	if out[0].AttachError == "" {
		t.Error("BAD should have AttachError populated")
	}
	if out[0].AttachmentKey != "" {
		t.Errorf("BAD.AttachmentKey = %q, want empty", out[0].AttachmentKey)
	}
	if out[1].AttachmentKey != "ATTACHOK" {
		t.Errorf("OK.AttachmentKey = %q, want ATTACHOK — second finding must still run", out[1].AttachmentKey)
	}
	if len(fa.uploads) != 1 || fa.uploads[0] != "ATTACHOK" {
		t.Errorf("upload calls wrong: %v (failed create should not trigger upload)", fa.uploads)
	}
}

func TestAttach_RecordsUploadErrorAndContinues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writePDF(t, dir, "A.pdf", []byte("%PDF"))

	findings := []Finding{
		{ItemKey: "ABC", DownloadedPath: p},
	}
	fa := newFakeAttacher()
	fa.createKeyByParent = map[string]string{"ABC": "ATT1"}
	fa.uploadErrByKey = map[string]error{"ATT1": errors.New("s3 403")}

	out, err := Attach(context.Background(), fa, findings, AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].AttachError == "" {
		t.Error("want AttachError set on upload failure")
	}
	// Attachment item got created server-side even though upload failed — we
	// record the key so the caller can show "attachment created but file
	// missing — retry with --refresh" rather than leave a ghost.
	if out[0].AttachmentKey != "ATT1" {
		t.Errorf("AttachmentKey = %q, want ATT1 (created but not uploaded)", out[0].AttachmentKey)
	}
}

func TestAttach_FiresCallbacksPerFinding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := writePDF(t, dir, "A.pdf", []byte("%PDF"))

	findings := []Finding{
		{ItemKey: "A", DownloadedPath: p},
		{ItemKey: "B"}, // skipped — no download
		{ItemKey: "C", DownloadedPath: p},
	}
	fa := newFakeAttacher()
	fa.createKeyByParent = map[string]string{"A": "KA", "C": "KC"}

	var starts, dones []string
	opts := AttachOptions{
		OnStart: func(_, total int, f Finding) {
			starts = append(starts, f.ItemKey)
			if total != 2 {
				t.Errorf("total should count only attachable findings, got %d", total)
			}
		},
		OnDone: func(_, _ int, f Finding) {
			dones = append(dones, f.ItemKey)
		},
	}
	if _, err := Attach(context.Background(), fa, findings, opts); err != nil {
		t.Fatal(err)
	}
	// Only the two with DownloadedPath should fire callbacks; skips don't.
	if got, want := sliceAsCSV(starts), "A,C"; got != want {
		t.Errorf("starts = %q, want %q", got, want)
	}
	if got, want := sliceAsCSV(dones), "A,C"; got != want {
		t.Errorf("dones = %q, want %q", got, want)
	}
}

// sliceAsCSV is a tiny helper kept local to this file — avoids dragging in
// strings.Join from other tests' helper surfaces.
func sliceAsCSV(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
