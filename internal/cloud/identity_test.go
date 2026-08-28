package cloud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// setHFToken swaps the local-credential hook for a test and restores it on
// cleanup. Without this every test in this file would shell out to `hf`.
func setHFToken(t *testing.T, token string, err error) {
	t.Helper()
	orig := hfTokenFn
	hfTokenFn = func() (string, error) { return token, err }
	t.Cleanup(func() { hfTokenFn = orig })
}

// serveWhoami stands up a fake Hub whose /api/whoami-v2 returns body with
// status, points HF_ENDPOINT at it, and reports the Authorization header the
// last request carried.
func serveWhoami(t *testing.T, status int, body string) *string {
	t.Helper()
	gotAuth := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/whoami-v2" {
			t.Errorf("requested %q, want /api/whoami-v2", r.URL.Path)
		}
		*gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HF_ENDPOINT", srv.URL)
	return gotAuth
}

const whoamiOK = `{"name":"alice","fullname":"Alice A","orgs":[{"name":"other"},{"name":"sciminds"}]}`

func TestHFToken_PrefersEnvOverCLI(t *testing.T) {
	t.Setenv("HF_TOKEN", "hf_from_env")
	// The CLI hook would return something else; the env var must win, and it
	// must win without paying the subprocess cost.
	setHFToken(t, "hf_from_cli", nil)

	got, err := hfToken()
	if err != nil {
		t.Fatalf("hfToken() error = %v", err)
	}
	if got != "hf_from_env" {
		t.Errorf("hfToken() = %q, want %q (HF_TOKEN must take precedence)", got, "hf_from_env")
	}
}

func TestHFWhoami_ParsesUserAndOrgs(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	gotAuth := serveWhoami(t, http.StatusOK, whoamiOK)

	user, orgs, err := hfWhoami()
	if err != nil {
		t.Fatalf("hfWhoami() error = %v", err)
	}
	if user != "alice" {
		t.Errorf("user = %q, want %q", user, "alice")
	}
	if len(orgs) != 2 || orgs[0] != "other" || orgs[1] != "sciminds" {
		t.Errorf("orgs = %v, want [other sciminds]", orgs)
	}
	if want := "Bearer hf_abc"; *gotAuth != want {
		t.Errorf("Authorization = %q, want %q", *gotAuth, want)
	}
}

func TestHFWhoami_UnauthorizedReadsAsNotLoggedIn(t *testing.T) {
	setHFToken(t, "hf_stale", nil)
	serveWhoami(t, http.StatusUnauthorized, `{"error":"Invalid credentials"}`)

	_, _, err := hfWhoami()
	if err == nil {
		t.Fatal("hfWhoami() error = nil, want an error for a 401")
	}
	// Verify maps this to ErrNotAuthenticated via isNotLoggedIn, so the
	// wording matters as much as the failure.
	if !isNotLoggedIn(err) {
		t.Errorf("isNotLoggedIn(%v) = false, want true so Verify reports ErrNotAuthenticated", err)
	}
}

func TestHFWhoami_NoTokenSkipsTheNetwork(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	setHFToken(t, "", nil)
	t.Setenv("HF_ENDPOINT", "http://127.0.0.1:1") // any request here would fail loudly

	_, _, err := hfWhoami()
	if !isNotLoggedIn(err) {
		t.Errorf("hfWhoami() error = %v, want a not-logged-in error", err)
	}
}

func TestTokenPresent(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	setHFToken(t, "hf_abc", nil)
	if !TokenPresent() {
		t.Error("TokenPresent() = false, want true when a credential is available")
	}

	setHFToken(t, "", errors.New("not logged in"))
	if TokenPresent() {
		t.Error("TokenPresent() = true, want false when `hf auth token` fails")
	}
}

func TestVerify_NoTokenIsNotAuthenticated(t *testing.T) {
	t.Setenv("HF_TOKEN", "")
	setHFToken(t, "", errors.New("not logged in"))
	t.Setenv("HF_ENDPOINT", "http://127.0.0.1:1")

	_, err := Verify()
	if !errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("Verify() error = %v, want ErrNotAuthenticated", err)
	}
}

func TestVerify_OK(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	serveWhoami(t, http.StatusOK, whoamiOK)

	cfg, err := Verify()
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if cfg.Username != "alice" || cfg.Org != DefaultOrg {
		t.Errorf("Verify() = %+v, want {alice %s}", cfg, DefaultOrg)
	}
}

func TestVerify_NotInOrg(t *testing.T) {
	setHFToken(t, "hf_abc", nil)
	serveWhoami(t, http.StatusOK, `{"name":"bob","orgs":[{"name":"other"}]}`)

	_, err := Verify()
	if err == nil {
		t.Fatal("Verify() error = nil, want an org-membership failure")
	}
	if errors.Is(err, ErrNotAuthenticated) {
		t.Errorf("Verify() error = %v, want an org failure, not ErrNotAuthenticated", err)
	}
	if !strings.Contains(err.Error(), DefaultOrg) || !strings.Contains(err.Error(), "bob") {
		t.Errorf("Verify() error = %q, want it to name both the user and the %s org", err, DefaultOrg)
	}
}

func TestHFRunError_NamesTheTimeout(t *testing.T) {
	// A deadline kill surfaces from os/exec as a bare "signal: killed", which
	// tells the user nothing about what went wrong.
	err := hfRunError(context.DeadlineExceeded, []string{"buckets", "ls"}, errors.New("signal: killed"), "")
	if err == nil {
		t.Fatal("hfRunError() = nil, want an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hfRunError() = %v, want it to wrap context.DeadlineExceeded", err)
	}
	if strings.Contains(err.Error(), "signal: killed") {
		t.Errorf("hfRunError() = %q, must not surface the bare signal text", err)
	}
	if !strings.Contains(err.Error(), "hf buckets ls") {
		t.Errorf("hfRunError() = %q, want it to name the command", err)
	}
}

func TestHFRunError_PassesThroughRealFailures(t *testing.T) {
	err := hfRunError(nil, []string{"buckets", "ls"}, errors.New("exit status 1"), "Repository Not Found")
	if !strings.Contains(err.Error(), "Repository Not Found") {
		t.Errorf("hfRunError() = %q, want stderr preserved for callers that pattern-match it", err)
	}
}
