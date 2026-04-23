package connector

// Import orchestrates the desktop-connector drag-drop equivalent:
// ping → upload → poll for recognize. Designed around an injectable
// Transport so orchestration logic is unit-tested without HTTP; the real
// *Client (client.go) satisfies Transport via method pointer.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Transport is the narrow interface Import depends on. *Client satisfies it.
type Transport interface {
	Ping(ctx context.Context) error
	SaveStandaloneAttachment(ctx context.Context, body io.Reader, meta SaveMeta) (*SaveResp, error)
	GetRecognizedItem(ctx context.Context, sessionID string) (*RecognizedResp, error)
}

// Options controls Import's waiting behavior.
//
//   - Timeout caps how long we'll block on desktop's getRecognizedItem call.
//     Zero means DefaultTimeout. Recognition that exceeds the timeout yields
//     a partial Result (err == nil, Recognized == false, Message explains).
//     The recognition may still finish in desktop after we give up.
//   - NoWait skips the recognize call entirely, returning immediately after
//     the upload is accepted. Useful for scripts that don't care about the
//     recognition outcome.
type Options struct {
	Timeout time.Duration
	NoWait  bool
}

// DefaultTimeout matches the user's expectation for a drag-drop-scale
// operation. Recognition typically completes in 2–10s on a warm desktop;
// 60s covers cold starts and slower networks to CrossRef.
const DefaultTimeout = 60 * time.Second

// Result is what Import hands back. The message is a short human-facing
// sentence suitable for the CLI's Result.Human() line; JSON consumers get
// the same summary under "message" plus the structured fields.
type Result struct {
	Path       string
	Recognized bool
	Title      string
	ItemType   string
	Message    string
}

// Import runs the connector flow end to end for a single file. The path
// must point to a readable PDF; the entire file is streamed to desktop. ctx
// cancellation aborts the blocking recognize call cleanly. Options zero
// values pick up the DefaultTimeout constant above.
//
// Composition note: Import = openPDF + Ping + importOne. ImportBatch reuses
// openPDF and importOne directly, pinging once for the whole batch instead
// of per file.
func Import(ctx context.Context, t Transport, path string, opts Options) (*Result, error) {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}

	f, err := openPDF(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if err := t.Ping(ctx); err != nil {
		return nil, err
	}
	return importOne(ctx, t, f, path, opts)
}

// openPDF opens path and verifies it's a non-empty regular file with a .pdf
// extension. The Stat checks catch the most common user mistakes (passed a
// directory, empty placeholder file) with a clear error rather than letting
// Read fail later with EISDIR or silently uploading 0 bytes. Extension
// rejection keeps non-PDFs out of the connector flow entirely — desktop's
// recognize pipeline only fires for PDFs and sending other types as
// application/pdf would surface as a confusing "couldn't identify" miss.
// Caller owns the *os.File and must Close it.
func openPDF(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}
	if fi.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%q is a directory; pass a single PDF file or a directory containing PDFs", path)
	}
	if fi.Size() == 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%q is empty", path)
	}
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		_ = f.Close()
		return nil, fmt.Errorf("%q is not a PDF; the desktop connector only supports PDFs (use `zot item add --file` for other types)", path)
	}
	return f, nil
}

// importOne uploads an already-opened+validated PDF and (optionally) waits
// for recognition. Caller is responsible for Ping and for Close — splitting
// these out lets ImportBatch ping once and reuse the per-file core without
// re-validating the path on every iteration.
func importOne(ctx context.Context, t Transport, f *os.File, path string, opts Options) (*Result, error) {
	sessionID, err := newSessionID()
	if err != nil {
		return nil, err
	}

	abs, _ := filepath.Abs(path)
	meta := SaveMeta{
		SessionID: sessionID,
		URL:       BuildFileURL(abs),
		Title:     filepath.Base(path),
	}

	save, err := t.SaveStandaloneAttachment(ctx, f, meta)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}

	base := &Result{Path: path}
	switch {
	case !save.CanRecognize:
		base.Message = "desktop imported the file but did not run metadata recognition (autoRecognizeFiles disabled or file not recognizable)"
		return base, nil
	case opts.NoWait:
		base.Message = "upload accepted; recognition not awaited (--no-wait)"
		return base, nil
	}

	// getRecognizedItem blocks server-side until session.autoRecognizePromise
	// resolves — a deadline on ctx is our only timeout lever. On ctx timeout
	// we return a partial result rather than an error; recognition may still
	// finish in desktop and the user can confirm via `zot search <title>`.
	recCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	rec, err := t.GetRecognizedItem(recCtx, sessionID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			base.Message = fmt.Sprintf("recognition did not complete within %s — check Zotero desktop for the imported attachment", opts.Timeout)
			return base, nil
		}
		return nil, fmt.Errorf("recognize: %w", err)
	}
	if !rec.Recognized {
		base.Message = "desktop ran recognition but couldn't identify the document (no CrossRef/arXiv match)"
		return base, nil
	}
	base.Recognized = true
	base.Title = rec.Title
	base.ItemType = rec.ItemType
	base.Message = fmt.Sprintf("recognized as %q (%s)", rec.Title, rec.ItemType)
	return base, nil
}

// newSessionID generates a 16-byte hex identifier. The real Zotero connector
// uses UUIDs; a 32-char hex string is equally unique on a single machine and
// avoids pulling in a UUID dependency for one field.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
