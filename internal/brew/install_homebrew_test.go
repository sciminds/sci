package brew

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// InstallHomebrew should no-op (return ErrHomebrewInstalled) when brew is
// already on PATH, so running it on a developer machine or CI runner with
// preinstalled brew doesn't re-trigger the installer.
func TestInstallHomebrew_AlreadyInstalled(t *testing.T) {
	if _, err := exec.LookPath("brew"); err != nil {
		t.Skip("brew not on PATH; this test only runs on machines with brew installed")
	}
	err := InstallHomebrew()
	if !errors.Is(err, ErrHomebrewInstalled) {
		t.Errorf("InstallHomebrew with brew present should return ErrHomebrewInstalled, got: %v", err)
	}
}

func TestFetchInstallScript_HappyPath(t *testing.T) {
	t.Parallel()
	const body = "#!/bin/bash\necho hi\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	got, err := fetchInstallScript(srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("fetchInstallScript: %v", err)
	}
	if string(got) != body {
		t.Errorf("body mismatch: got %q want %q", got, body)
	}
}

func TestFetchInstallScript_RejectsHTMLErrorPage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>404</body></html>"))
	}))
	t.Cleanup(srv.Close)

	_, err := fetchInstallScript(srv.URL, 1<<20)
	if err == nil {
		t.Fatal("expected error when body has no bash shebang")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Errorf("error should mention bash shebang: %v", err)
	}
}

func TestFetchInstallScript_RejectsNon200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := fetchInstallScript(srv.URL, 1<<20)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestFetchInstallScript_RejectsOversize(t *testing.T) {
	t.Parallel()
	// 200 bytes of valid bash, but cap at 50 — should fail.
	body := "#!/bin/bash\n" + strings.Repeat("x", 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	_, err := fetchInstallScript(srv.URL, 50)
	if err == nil {
		t.Fatal("expected error when body exceeds limit")
	}
}

func TestFetchInstallScript_AcceptsEnvShebang(t *testing.T) {
	t.Parallel()
	const body = "#!/usr/bin/env bash\necho hi\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchInstallScript(srv.URL, 1<<20); err != nil {
		t.Errorf("/usr/bin/env bash shebang should be accepted: %v", err)
	}
}

func TestInstallScriptClient_HasTimeout(t *testing.T) {
	t.Parallel()
	if installScriptClient.Timeout <= 0 {
		t.Errorf("installScriptClient.Timeout = %v, want >0 so a stalled server cannot hang the installer", installScriptClient.Timeout)
	}
}
