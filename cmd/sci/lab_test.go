package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/netutil"
	"github.com/sciminds/cli/internal/uikit"
)

// startSSHBannerListener spins a single-accept TCP server that writes a fake
// SSH identification banner — enough to pass lab.Preflight's banner-grab check
// (RFC 4253 §4.2). The listener and its accept goroutine are torn down via
// t.Cleanup, so callers only need to point lab.SetPreflightAddr at its addr.
func startSSHBannerListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("SSH-2.0-fakeserver\r\n"))
			_ = conn.Close()
		}
	}()
	return ln
}

func TestLabSetup_JSONRequiresUser(t *testing.T) {
	uikit.SetQuiet(false)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	ln := startSSHBannerListener(t)
	lab.SetPreflightAddr(ln.Addr().String())
	t.Cleanup(lab.ResetPreflightAddr)

	root := buildRoot()

	err := root.Run(context.Background(), []string{"sci", "--json", "lab", "setup"})

	if err == nil {
		t.Fatal("expected error when --json is set without --user")
	}
	if !strings.Contains(err.Error(), "--user") {
		t.Errorf("error should mention --user, got: %v", err)
	}
}

// TestLab_WarmsMasterBeforeSSH locks down the contract that every lab
// subcommand which shells out to ssh/rsync warms the ControlMaster first.
// Without this, a stale master leads to an unexpected Duo prompt mid-command
// (the failure mode noted while working on issue #2). We stub warmMaster to
// return a sentinel error and assert each command bails with that error —
// proof it went through the warm step before reaching the real exec path.
func TestLab_WarmsMasterBeforeSSH(t *testing.T) {
	uikit.SetQuiet(true)
	t.Cleanup(func() { uikit.SetQuiet(false) })

	// Point xdg.ConfigHome at a temp dir and seed a fake lab config so
	// RequireConfig() succeeds.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	if err := lab.SaveConfig(&lab.Config{User: "tester"}); err != nil {
		t.Fatal(err)
	}

	// Pass labCommand.Before's two checks: netutil.Online() and lab.Preflight().
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(probe.Close)
	netutil.SetProbeURL(probe.URL)
	t.Cleanup(netutil.ResetProbeURL)

	ln := startSSHBannerListener(t)
	lab.SetPreflightAddr(ln.Addr().String())
	t.Cleanup(lab.ResetPreflightAddr)

	// Stub the warm so we can prove each command went through it. A real
	// warm would attempt ssh to ssrde.ucsd.edu — which we definitely don't
	// want in a unit test.
	sentinel := errors.New("warmMaster-was-called")
	origWarm := warmMaster
	warmMaster = func(*lab.Config) error { return sentinel }
	t.Cleanup(func() { warmMaster = origWarm })

	// `put` requires a local file to exist before reaching the warm step.
	tmpFile := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(tmpFile, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		argv []string
	}{
		{"ls", []string{"sci", "lab", "ls"}},
		{"get", []string{"sci", "lab", "get", "data/foo"}},
		{"get-noarg", []string{"sci", "lab", "get"}},
		{"put", []string{"sci", "lab", "put", tmpFile}},
		{"connect", []string{"sci", "lab", "connect"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := buildRoot().Run(context.Background(), tc.argv)
			if !errors.Is(err, sentinel) {
				t.Errorf("%s: expected warmMaster sentinel, got %v", tc.name, err)
			}
		})
	}
}

// TestLab_BrowseRedirectsToGet locks in the user-facing redirect for the
// removed `sci lab browse` command. Muscle-memory typing should land on a
// tailored "use sci lab get instead" message, not the generic Levenshtein
// suggestion (which used to surface "connect" — actively misleading).
func TestLab_BrowseRedirectsToGet(t *testing.T) {
	// labCommand's Before probes netutil + lab.Preflight, but
	// RejectUnknownSubcommand runs first in the chain, so the redirect fires
	// before either probe. Stub both anyway so this test stays hermetic if
	// the chain order ever flips.
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(probe.Close)
	netutil.SetProbeURL(probe.URL)
	t.Cleanup(netutil.ResetProbeURL)

	ln := startSSHBannerListener(t)
	lab.SetPreflightAddr(ln.Addr().String())
	t.Cleanup(lab.ResetPreflightAddr)

	err := buildRoot().Run(context.Background(), []string{"sci", "lab", "browse"})
	if err == nil {
		t.Fatal("expected deprecation error")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Errorf("error should say the command was removed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "sci lab get") {
		t.Errorf("error should point to `sci lab get`, got: %v", err)
	}
}

func TestLabSetup_HasUserFlag(t *testing.T) {
	root := buildRoot()
	lab := findCmd(root.Commands, "lab")
	if lab == nil {
		t.Fatal("lab command not found")
	}
	setup := findCmd(lab.Commands, "setup")
	if setup == nil {
		t.Fatal("lab setup not found")
	}
	if !hasFlag(setup, "user") {
		t.Error("lab setup should have a --user flag")
	}
}
