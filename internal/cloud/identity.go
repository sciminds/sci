package cloud

// identity.go — resolving "who am I on the Hub, and in which orgs".
//
// The credential is read from the `hf` CLI (or HF_TOKEN) and the answer comes
// from the Hub's whoami endpoint over plain HTTP. That split is deliberate:
// `hf auth whoami` wraps the same endpoint but has been measured at 70-125s
// on a connection where the endpoint itself answers in 0.3s, and identity sits
// in front of every `sci cloud` command and the gui's cloud page. Reading the
// token stays delegated to `hf` so sci still stores no credential of its own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/samber/lo"
)

// hfAPITimeout bounds the whoami round-trip. Generous next to the ~0.3s the
// endpoint actually takes, but identity blocks a user-facing path, so it must
// fail rather than hang.
const hfAPITimeout = 10 * time.Second

// hfTokenTimeout bounds `hf auth token`. It reads the local token store with
// no network access; the budget covers Python interpreter startup only.
const hfTokenTimeout = 10 * time.Second

// errNoToken reports a genuinely absent local credential.
var errNoToken = errors.New("not logged in to Hugging Face: no token found")

// errTokenRejected reports a credential the Hub explicitly refused.
var errTokenRejected = errors.New("not logged in to Hugging Face: the stored token was rejected")

// hfTokenFn returns the credential stored by `hf auth login`. Local read, no
// network. Overridable in tests.
var hfTokenFn = func() (string, error) {
	if _, err := exec.LookPath("hf"); err != nil {
		return "", fmt.Errorf("hf not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hfTokenTimeout)
	defer cancel()
	// Stdout carries the token; stderr carries hints on success and the
	// reason on failure, so the two must not be mixed.
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, "hf", "auth", "token")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("hf auth token: %w", ctx.Err())
		}
		message := strings.ToLower(string(out) + stderr.String())
		if strings.Contains(message, "not logged in") || strings.Contains(message, "no token found") {
			return "", errNoToken
		}
		return "", fmt.Errorf("hf auth token: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// hfToken returns the Hugging Face credential, preferring HF_TOKEN over the
// stored login. That is huggingface_hub's own precedence, and honoring it
// keeps sci reading the same credential the `hf` CLI would use.
func hfToken() (string, error) {
	if tok := strings.TrimSpace(os.Getenv("HF_TOKEN")); tok != "" {
		return tok, nil
	}
	tok, err := hfTokenFn()
	if err != nil {
		if errors.Is(err, errNoToken) {
			return "", errNoToken
		}
		return "", fmt.Errorf("read Hugging Face token: %w", err)
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", errNoToken
	}
	return tok, nil
}

// TokenPresent reports whether a Hugging Face credential is available locally.
// This is the fast answer to "is the user logged in" — no network, so it stays
// honest when the Hub is slow or unreachable. [Verify] additionally confirms
// the credential is live and carries the org membership sci cloud requires.
func TokenPresent() bool {
	tok, err := hfToken()
	return err == nil && tok != ""
}

// Whoami returns the authenticated Hugging Face user and the orgs they belong
// to. It reads the local credential and asks the Hub, so it needs the network
// — callers wanting only "is the user logged in" should use [TokenPresent],
// which does not.
func Whoami() (user string, orgs []string, err error) {
	return hfWhoami()
}

func hfWhoami() (user string, orgs []string, err error) {
	tok, err := hfToken()
	if err != nil {
		return "", nil, err
	}
	return hfWhoamiWithToken(tok)
}

// hfWhoamiWithToken returns the authenticated user and org memberships from
// the Hub's whoami endpoint using the already-resolved credential.
var hfWhoamiWithToken = func(tok string) (user string, orgs []string, err error) {
	if tok == "" {
		return "", nil, errNoToken
	}
	ctx, cancel := context.WithTimeout(context.Background(), hfAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hfEndpoint()+"/api/whoami-v2", nil)
	if err != nil {
		return "", nil, fmt.Errorf("build whoami request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := hubDo(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("hugging face whoami: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := hubStatusError(resp, "hugging face whoami"); err != nil {
		return "", nil, err
	}

	var body hfWhoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", nil, fmt.Errorf("parse hugging face whoami response: %w", err)
	}
	if body.Name == "" {
		return "", nil, errors.New("hugging face whoami returned no username")
	}
	orgs = lo.Map(body.Orgs, func(o hfOrg, _ int) string { return o.Name })
	return body.Name, orgs, nil
}

// hfOrg is one org membership in a whoami response.
type hfOrg struct {
	Name string `json:"name"`
}

// hfWhoamiResponse is the subset of GET /api/whoami-v2 that sci reads.
type hfWhoamiResponse struct {
	Name string  `json:"name"`
	Orgs []hfOrg `json:"orgs"`
}
