package main

// cass_consent_test.go — proves the Canvas delete verbs never reach Canvas
// when --json cannot obtain consent.
//
// consent_json_test.go asserts that these commands *declare* a --yes flag.
// That is a shape check: it cannot tell whether the flag is actually consulted
// before the HTTP call, and all three verbs previously passed
// ConfirmOrSkip(false, …) — a literal that no flag could ever influence. The
// tests here drive the real Action against an httptest server and count what
// arrives, so a regression that reconnects the delete to the network fails
// here even if the flag is still declared.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/urfave/cli/v3"
)

// canvasRecorder is a stand-in Canvas that records every request it receives.
// A delete that was correctly refused leaves it empty.
type canvasRecorder struct {
	mu       sync.Mutex
	requests []string // "METHOD /path"
}

func (c *canvasRecorder) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, r.Method+" "+r.URL.Path)
}

func (c *canvasRecorder) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

// newCanvasProject stands up a fake Canvas, writes a cass.yaml pointing at it
// plus a credentials file holding a token, and moves the test into that
// directory — the three inputs cassCanvasContext reads.
func newCanvasProject(t *testing.T) *canvasRecorder {
	t.Helper()

	rec := &canvasRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		// Canvas answers a delete with the deleted object.
		_, _ = fmt.Fprint(w, `{"id":5,"name":"week 1","published":false}`)
	}))
	t.Cleanup(srv.Close)

	projDir := t.TempDir()
	cfg := fmt.Sprintf("canvas:\n  url: %s/courses/123\n", srv.URL)
	if err := os.WriteFile(filepath.Join(projDir, "cass.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write cass.yaml: %v", err)
	}

	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	sciDir := filepath.Join(xdgDir, "sci")
	if err := os.MkdirAll(sciDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	creds := `{"canvas_token":"fake-token-for-tests"}`
	if err := os.WriteFile(filepath.Join(sciDir, "credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	t.Chdir(projDir)
	return rec
}

// runCanvasCmd runs one `cass canvas <sub>` command under a root that arms
// quiet mode from --json, the way cmd/sci/root.go does.
func runCanvasCmd(t *testing.T, sub *cli.Command, args ...string) error {
	t.Helper()
	var jsonOut bool
	root := &cli.Command{
		Name:  "sci",
		Flags: []cli.Flag{cmdutil.JSONFlag(&jsonOut)},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			uikit.SetQuiet(cmdutil.IsJSON(cmd))
			return ctx, nil
		},
		Commands: []*cli.Command{sub},
	}
	t.Cleanup(func() { uikit.SetQuiet(false) })
	t.Setenv("SCI_ASSUME", "")
	return root.Run(context.Background(), slices.Concat([]string{"sci", sub.Name}, args))
}

// canvasDeleteVerbs are the three commands that delete something in Canvas
// behind a confirmation. All three shipped with ConfirmOrSkip(false, …).
func canvasDeleteVerbs() []struct {
	name string
	cmd  func() *cli.Command
} {
	return []struct {
		name string
		cmd  func() *cli.Command
	}{
		{"modules", cassCanvasModulesCommand},
		{"assignments", cassCanvasAssignmentsCommand},
		{"announce", cassCanvasAnnounceCommand},
	}
}

func TestCanvasDelete_JSONWithoutConsentNeverReachesCanvas(t *testing.T) {
	for _, verb := range canvasDeleteVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			rec := newCanvasProject(t)

			err := runCanvasCmd(t, verb.cmd(), "--json", "--delete", "5")
			if err == nil {
				t.Fatalf("%s --delete --json returned nil; it deleted without consent", verb.name)
			}
			if !errors.Is(err, cmdutil.ErrNeedsConsent) {
				t.Fatalf("want ErrNeedsConsent, got %T: %v", err, err)
			}
			if seen := rec.seen(); len(seen) != 0 {
				t.Errorf("refused delete still called Canvas: %v", seen)
			}
		})
	}
}

func TestCanvasDelete_JSONWithYesDeletes(t *testing.T) {
	for _, verb := range canvasDeleteVerbs() {
		t.Run(verb.name, func(t *testing.T) {
			rec := newCanvasProject(t)

			if err := runCanvasCmd(t, verb.cmd(), "--json", "--yes", "--delete", "5"); err != nil {
				t.Fatalf("%s --delete --json --yes: %v", verb.name, err)
			}
			seen := rec.seen()
			if len(seen) == 0 {
				t.Fatalf("--yes consented but no request reached Canvas — the flag is not wired to the delete")
			}
			sawDelete := slices.ContainsFunc(seen, func(req string) bool {
				return strings.HasPrefix(req, "DELETE ")
			})
			if !sawDelete {
				t.Errorf("no DELETE among %v", seen)
			}
		})
	}
}
