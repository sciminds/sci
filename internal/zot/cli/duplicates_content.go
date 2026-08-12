package cli

// duplicates_content.go — the disk half of `doctor duplicates
// --strategy content`. The clustering itself is pure and lives in
// internal/zot/hygiene; hashing every PDF in the library is I/O, so it
// stays here and is handed over as a plain map.

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/extract"
	"github.com/sciminds/sci/pkg/local"
)

// hashLibraryPDFs is [buildContentKeys] behind a spinner. The scan
// reads a megabyte per PDF across the whole library — tens of seconds
// on a few thousand papers — so it must say what it is doing rather
// than look hung. The spinner suppresses itself under --json and off a
// terminal.
func hashLibraryPDFs(ctx context.Context, db local.Reader, dataDir string) (map[string]string, error) {
	var keys map[string]string
	err := uikit.RunWithSpinnerStatus("Hashing library PDFs...", func(setStatus func(string)) error {
		var err error
		keys, err = buildContentKeys(ctx, db, dataDir, func(done, total int) {
			setStatus(fmt.Sprintf("%d/%d", done, total))
		})
		return err
	})
	return keys, err
}

// contentKeyJobs bounds the hashing fan-out. [extract.HashPDF] reads
// the first MiB of each file, so this is disk-bound, not CPU-bound —
// more workers past a handful just queue up behind the same device.
func contentKeyJobs() int { return min(runtime.NumCPU(), 8) }

// buildContentKeys hashes every parent item's PDF and returns item key
// → [extract.ContentKey]. Items whose PDF is missing or unreadable are
// omitted rather than mapped to "": an absent key means "not a
// candidate", and the clusterer refuses to group on empty anyway, so a
// hash failure can never be mistaken for a match.
//
// progress, when non-nil, is called after each file with the running
// count. It may be called from any goroutine, one call at a time.
func buildContentKeys(ctx context.Context, db local.Reader, dataDir string, progress func(done, total int)) (map[string]string, error) {
	parents, err := db.ListAllPDFAttachments()
	if err != nil {
		return nil, err
	}

	var (
		mu   sync.Mutex
		out  = make(map[string]string, len(parents))
		done int
	)
	sem := make(chan struct{}, contentKeyJobs())
	var wg sync.WaitGroup

	for _, p := range parents {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			att := p.Attachment
			key := extract.ContentKey(hashOrEmpty(zot.AttachmentPath(dataDir, &local.Attachment{
				Key:      att.Key,
				Filename: att.Filename,
			})))

			mu.Lock()
			defer mu.Unlock()
			done++
			if key != "" {
				out[p.ParentKey] = key
			}
			if progress != nil {
				progress(done, len(parents))
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// hashOrEmpty is [extract.HashPDF] with the error folded into an empty
// string. Every failure mode here — file gone, unreadable, permissions
// — means the same thing to the caller: this item has no content
// identity to compare.
func hashOrEmpty(path string) string {
	hash, err := extract.HashPDF(path)
	if err != nil {
		return ""
	}
	return hash
}
