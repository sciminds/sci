package main

// cloud_consent_test.go — proves `sci cloud put` never overwrites a remote
// object that it could not get consent to overwrite.
//
// `--json` used to imply --force here, so an agent uploading under --json
// silently clobbered whatever already sat at that name. The fix made --force
// the only consent, and made an unanswerable prompt return
// cmdutil.ErrNeedsConsent. Neither half is provable by reading the flag list:
// the question is whether anything reached the bucket. These tests swap the
// upload path's client for a recorder (share.SetPutClientFactory) and count.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/share"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

// putRecorder is a stand-in bucket client that records every call the upload
// path makes. A `put` that was correctly refused leaves [putRecorder.writes]
// empty while still showing the existence check in [putRecorder.seen].
type putRecorder struct {
	mu sync.Mutex
	// exists is the answer both existence checks give.
	exists bool
	calls  []string // "Exists results.csv", "Upload results.csv", …
}

func (r *putRecorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

// seen returns every recorded call, in order.
func (r *putRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// writes returns only the calls that would have changed the remote.
func (r *putRecorder) writes() []string {
	return slices.DeleteFunc(r.seen(), func(call string) bool {
		return !strings.HasPrefix(call, "Upload ") && !strings.HasPrefix(call, "SyncUp ")
	})
}

func (r *putRecorder) Exists(_ context.Context, name string) (bool, error) {
	r.record("Exists " + name)
	return r.exists, nil
}

func (r *putRecorder) PrefixExists(_ context.Context, name string) (bool, error) {
	r.record("PrefixExists " + name)
	return r.exists, nil
}

func (r *putRecorder) Upload(_ context.Context, name string, body io.Reader) error {
	r.record("Upload " + name)
	_, err := io.Copy(io.Discard, body)
	return err
}

func (r *putRecorder) SyncUp(_ context.Context, _, name string) error {
	r.record("SyncUp " + name)
	return nil
}

func (r *putRecorder) PublicObjectURL(string) string { return "" }

// newPutRecorder installs a recorder as the upload path's client for the rest
// of the test. exists is what both remote existence checks answer.
func newPutRecorder(t *testing.T, exists bool) *putRecorder {
	t.Helper()
	rec := &putRecorder{exists: exists}
	share.SetPutClientFactory(func(string) (share.PutClient, error) { return rec, nil })
	t.Cleanup(share.ResetPutClientFactory)
	return rec
}

// runCloudPut runs `sci cloud put <args>` under a root that arms quiet mode
// from --json, the way cmd/sci/root.go does. The `cloud` parent is skipped so
// its online-check Before never fires.
func runCloudPut(t *testing.T, args ...string) error {
	t.Helper()
	var jsonOut bool
	put := cloudPutCommand()
	root := &cli.Command{
		Name:  "sci",
		Flags: []cli.Flag{cmdutil.JSONFlag(&jsonOut)},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			return ctx, nil
		},
		Commands: []*cli.Command{put},
	}
	t.Cleanup(func() { uikit.SetQuiet(false) })
	t.Setenv("SCI_ASSUME", "")
	return root.Run(context.Background(), slices.Concat([]string{"sci", put.Name}, args))
}

// newUploadSource creates a file (or directory) in a temp dir and chdirs there,
// so the upload name is its bare basename.
func newUploadSource(t *testing.T, name string, isDir bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if isDir {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(path, "a.txt"), []byte("hi\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	} else if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Chdir(dir)
	return name
}

func TestCloudPut_JSONWithoutForceNeverWrites(t *testing.T) {
	cases := []struct {
		name      string
		isDir     bool
		wantCheck string
	}{
		{"results.csv", false, "Exists results.csv"},
		{"myrepo", true, "PrefixExists myrepo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newPutRecorder(t, true)
			src := newUploadSource(t, tc.name, tc.isDir)

			err := runCloudPut(t, "--json", src)
			if err == nil {
				t.Fatalf("put --json over an existing %q returned nil; it overwrote without consent", tc.name)
			}
			if !errors.Is(err, cmdutil.ErrNeedsConsent) {
				t.Fatalf("want ErrNeedsConsent, got %T: %v", err, err)
			}
			if w := rec.writes(); len(w) != 0 {
				t.Errorf("refused put still wrote to the bucket: %v", w)
			}
			// Without this the test would also pass if the guard never ran and
			// the error came from somewhere else entirely.
			if seen := rec.seen(); !slices.Contains(seen, tc.wantCheck) {
				t.Errorf("guard never ran %q; calls were %v", tc.wantCheck, seen)
			}
		})
	}
}

// TestCloudPut_JSONWithConsentUploads covers --force and each of its aliases,
// so a rename or a dropped alias can't silently strip the only way to consent
// under --json.
func TestCloudPut_JSONWithConsentUploads(t *testing.T) {
	for _, flag := range []string{"--force", "-f", "--yes", "-y"} {
		t.Run(flag, func(t *testing.T) {
			rec := newPutRecorder(t, true)
			src := newUploadSource(t, "results.csv", false)

			if err := runCloudPut(t, "--json", flag, src); err != nil {
				t.Fatalf("put --json %s: %v", flag, err)
			}
			if w := rec.writes(); !slices.Contains(w, "Upload results.csv") {
				t.Fatalf("%s consented but nothing was uploaded; calls were %v", flag, rec.seen())
			}
		})
	}
}

// TestCloudPut_JSONWithoutForceUploadsWhenAbsent guards the other direction:
// the consent gate must not turn a plain first upload into a refusal.
func TestCloudPut_JSONWithoutForceUploadsWhenAbsent(t *testing.T) {
	rec := newPutRecorder(t, false)
	src := newUploadSource(t, "results.csv", false)

	if err := runCloudPut(t, "--json", src); err != nil {
		t.Fatalf("put --json onto an empty name: %v", err)
	}
	if w := rec.writes(); !slices.Contains(w, "Upload results.csv") {
		t.Fatalf("nothing uploaded; calls were %v", rec.seen())
	}
}
