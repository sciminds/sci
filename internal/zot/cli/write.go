package cli

// The write half of the surface. Every verb that changes the library goes
// through requireAPIClient, which is where the four preconditions live in one
// place: a configured local library, a stored API key, a network, and a
// single target library. The verbs themselves live in their own files
// (collection_write.go, item_write.go, …); this file is the door they share.

import (
	"context"
	"errors"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/netutil"
	"github.com/sciminds/sci/internal/zot"
	"github.com/sciminds/sci/internal/zot/api"
)

// setupAPIKeyFix is the one command every credential failure points at.
const setupAPIKeyFix = "sci zot setup --api-key <key>"

// requireAPIConfigCoded wraps zot.RequireAPIConfig, upgrading its two
// sentinels to CodedErrors with the setup command as the fix. The
// not-configured case is the same one every read reports; the no-credential
// case is specific to writes and says so, because a user who can search their
// library has no reason to expect a write to fail.
func requireAPIConfigCoded() (*zot.Config, error) {
	cfg, err := zot.RequireAPIConfig()
	if err != nil {
		switch {
		case errors.Is(err, zot.ErrNoCredential):
			return nil, cmdutil.Coded(cmdutil.CodeNotConfigured, "%v", err).
				WithFix(setupAPIKeyFix).
				WithTry("create a key with library write access at https://www.zotero.org/settings/keys/new, then re-run setup with it; every read verb keeps working without one")
		case errors.Is(err, zot.ErrNotConfigured):
			return nil, cmdutil.Coded(cmdutil.CodeNotConfigured, "%v", err).
				WithFix("sci zot setup")
		}
		return nil, err
	}
	return cfg, nil
}

// requireAPIClient builds the Web API client a write verb needs, or explains
// exactly which precondition is missing.
//
// Order matters for the message the user gets: configuration first (fixable
// offline), then the network (the local reads still work — the Try says so),
// then the library scope. `--library all` is rejected here rather than in
// every verb because the merged read pool has no server-side counterpart: a
// write targets one library.
func requireAPIClient(ctx context.Context) (*api.Client, error) {
	cfg, err := requireAPIConfigCoded()
	if err != nil {
		return nil, err
	}
	if !netutil.Online() {
		return nil, cmdutil.Coded(cmdutil.CodeOffline, "no internet connection — writing to Zotero needs the Web API").
			WithTry("re-run when online; every read verb (search, item read, bib, export, doctor) works offline")
	}
	ref, err := ensureLibraryScope(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if ref.Scope == zot.LibAll {
		return nil, cmdutil.Coded(cmdutil.CodeUsage, "--library all is a merged read pool — a write targets one library").
			WithTry("re-run with --library personal or --library shared")
	}
	c, err := api.New(credentialsFor(cfg), ref)
	if err != nil {
		// The constructor validates what setup stored (numeric user and
		// group IDs), so a failure here means the file was edited by hand.
		return nil, cmdutil.Coded(cmdutil.CodeNotConfigured, "stored Zotero credential is unusable: %v", err).
			WithFix(setupAPIKeyFix)
	}
	return c, nil
}

// credentialsFor projects the stored config onto what the client needs.
func credentialsFor(cfg *zot.Config) api.Credentials {
	return api.Credentials{APIKey: cfg.APIKey, UserID: cfg.UserID, GroupID: cfg.SharedGroupID}
}
