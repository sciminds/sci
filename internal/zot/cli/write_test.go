package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/zot"
)

// The door every write verb goes through refuses in a fixed order —
// configuration, credential, network, scope — and each refusal names its
// remedy. These tests pin the order and the remedies; the verbs behind the
// door are tested against a fake Zotero in their own files.

func codedErr(t *testing.T, err error) *cmdutil.CodedError {
	t.Helper()
	coded, ok := errors.AsType[*cmdutil.CodedError](err)
	if !ok {
		t.Fatalf("want a CodedError, got %T: %v", err, err)
	}
	return coded
}

func TestRequireAPIClient_NotConfiguredNamesSetup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	_, err := requireAPIClient(context.Background())
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotConfigured || coded.Fix != "sci zot setup" {
		t.Errorf("got %+v", coded)
	}
}

// TestRequireAPIClient_NoCredentialNamesTheAPIKeyFix: a library that reads
// fine but holds no key is the common case, and the fix is the one-liner
// that adds one — not a full re-setup.
func TestRequireAPIClient_NoCredentialNamesTheAPIKeyFix(t *testing.T) {
	withTestConfig(t, "42", "6506098")
	_, err := requireAPIClient(context.Background())
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeNotConfigured {
		t.Errorf("Code = %q, want not-configured", coded.Code)
	}
	if !strings.Contains(coded.Fix, "--api-key") {
		t.Errorf("Fix = %q, want the --api-key setup command", coded.Fix)
	}
	if !strings.Contains(coded.Message, "reads work without one") {
		t.Errorf("Message should reassure that reads are unaffected: %q", coded.Message)
	}
}

func TestRequireAPIClient_OfflineIsCodeOffline(t *testing.T) {
	dataDir := withTestConfig(t, "42", "6506098")
	saveWithCredential(t, dataDir)
	netutil.SetProbeURL("http://127.0.0.1:1/unreachable")
	t.Cleanup(netutil.ResetProbeURL)

	_, err := requireAPIClient(context.Background())
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeOffline {
		t.Errorf("Code = %q, want offline", coded.Code)
	}
	if !strings.Contains(coded.Try, "offline") {
		t.Errorf("Try should say reads still work offline: %q", coded.Try)
	}
}

// TestRequireAPIClient_RejectsLibraryAll: the merged read pool has no
// server-side counterpart, so a write under --library all is a usage error
// with the single-library rewrite, not a write against a guessed library.
func TestRequireAPIClient_RejectsLibraryAll(t *testing.T) {
	dataDir := withTestConfig(t, "42", "6506098")
	saveWithCredential(t, dataDir)
	withOnlineProbe(t)

	ctx := withLibraryHolder(context.Background(), &libraryHolder{HasFlag: true, Partial: zot.LibAll})
	_, err := requireAPIClient(ctx)
	coded := codedErr(t, err)
	if coded.Code != cmdutil.CodeUsage || !strings.Contains(coded.Try, "--library personal") {
		t.Errorf("got %+v", coded)
	}
}

func TestRequireAPIClient_BuildsAClientBoundToTheScope(t *testing.T) {
	dataDir := withTestConfig(t, "42", "6506098")
	saveWithCredential(t, dataDir)
	withOnlineProbe(t)

	ctx := withLibraryHolder(context.Background(), &libraryHolder{HasFlag: true, Partial: zot.LibShared})
	c, err := requireAPIClient(ctx)
	if err != nil {
		t.Fatalf("requireAPIClient: %v", err)
	}
	if c.Library().Scope != zot.LibShared {
		t.Errorf("client bound to %q, want shared", c.Library().Scope)
	}
}

// saveWithCredential stores a key + user ID on top of the test config so the
// credential gate passes.
func saveWithCredential(t *testing.T, dataDir string) {
	t.Helper()
	if err := zot.SaveConfig(&zot.Config{DataDir: dataDir, SharedGroupID: "6506098", SharedGroupName: "sciminds", APIKey: "zABCDEFGHIJKLMNOPQRSTUVW", UserID: "42"}); err != nil {
		t.Fatal(err)
	}
}

// withOnlineProbe points netutil.Online at a local server that answers, so
// the network gate passes without the network. The probe URL is a package
// global, so callers must not t.Parallel().
func withOnlineProbe(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	netutil.SetProbeURL(srv.URL)
	t.Cleanup(netutil.ResetProbeURL)
}
