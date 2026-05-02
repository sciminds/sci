package cli

// Library scope resolution + scope-aware output.
//
// `--library personal|shared` is required by every leaf that touches the
// library. Three cases need handling, all gated through ensureLibraryScope:
//
//  1. Flag set      → use it (lazy-detect shared via API on first use).
//  2. Flag unset, only personal configured → auto-select, log it once.
//  3. Flag unset, both configured + interactive → uikit.Select.
//  4. Flag unset, both configured + --json → hard error (non-interactive).
//
// The resolved ref is cached on a *libraryHolder pinned to ctx by
// ResolveLibraryBefore; outputScoped reads it back to surface the active
// library in every command's output.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/api"
	"github.com/urfave/cli/v3"
)

// libraryScopePrompter is the interactive picker invoked when --library is
// unset and both libraries are configured. Production wires uikitLibraryPrompter;
// tests override via the package-level defaultLibraryPrompter var.
type libraryScopePrompter func(opts []zot.LibraryScope) (zot.LibraryScope, error)

// defaultLibraryPrompter is the prompter ensureLibraryScope uses by default.
// Tests swap this for a deterministic stub.
var defaultLibraryPrompter libraryScopePrompter = uikitLibraryPrompter

func uikitLibraryPrompter(opts []zot.LibraryScope) (zot.LibraryScope, error) {
	huhOpts := make([]huh.Option[zot.LibraryScope], 0, len(opts))
	for _, s := range opts {
		huhOpts = append(huhOpts, huh.NewOption(string(s), s))
	}
	return uikit.Select("Pick a Zotero library (no --library was passed)", huhOpts)
}

// libraryHolder is the mutable scope state pinned to ctx by ResolveLibraryBefore.
// HasFlag/Partial capture what --library said; JSONMode controls the
// non-interactive error path; Resolved memoizes the first ensureLibraryScope.
type libraryHolder struct {
	HasFlag  bool
	Partial  zot.LibraryScope
	JSONMode bool
	Resolved *zot.LibraryRef
}

type libraryHolderKey struct{}

func libraryHolderFromCtx(ctx context.Context) *libraryHolder {
	h, _ := ctx.Value(libraryHolderKey{}).(*libraryHolder)
	return h
}

// withLibraryHolder pins a holder on ctx. ResolveLibraryBefore is the only
// production caller; tests use it to seed bespoke holders.
func withLibraryHolder(ctx context.Context, h *libraryHolder) context.Context {
	return context.WithValue(ctx, libraryHolderKey{}, h)
}

// ensureLibraryScope returns the resolved LibraryRef for the current command.
// Memoized on the holder; safe to call multiple times per action.
//
// Cases (see file-level doc comment):
//
//   - holder absent → fall back to the original "--library required" error
//     for callers (tests) that bypassed the Before hook.
//   - HasFlag=true → resolve via Config.ResolveWithProbe, identical to the
//     pre-refactor path.
//   - HasFlag=false, only personal configured → auto-select personal.
//   - HasFlag=false, both configured + JSONMode → error (non-interactive).
//   - HasFlag=false, both configured + interactive → prompt via
//     defaultLibraryPrompter, then resolve.
func ensureLibraryScope(ctx context.Context, cfg *zot.Config) (zot.LibraryRef, error) {
	h := libraryHolderFromCtx(ctx)
	if h == nil {
		return zot.LibraryRef{}, fmt.Errorf("--library is required (values: personal, shared)")
	}
	if h.Resolved != nil {
		return *h.Resolved, nil
	}

	var (
		ref zot.LibraryRef
		err error
	)
	switch {
	case h.HasFlag:
		ref, err = resolveScopeWithProbe(ctx, cfg, h.Partial)
	default:
		ref, err = autoOrPromptScope(ctx, cfg, h.JSONMode)
	}
	if err != nil {
		return zot.LibraryRef{}, err
	}
	h.Resolved = &ref
	return ref, nil
}

// autoOrPromptScope handles the no-flag case.
func autoOrPromptScope(ctx context.Context, cfg *zot.Config, jsonMode bool) (zot.LibraryRef, error) {
	sharedConfigured := cfg.SharedGroupID != ""

	if !sharedConfigured {
		ref, err := resolveScopeWithProbe(ctx, cfg, zot.LibPersonal)
		if err != nil {
			return zot.LibraryRef{}, err
		}
		fmt.Fprintf(os.Stderr, "  %s --library not set; auto-selected personal (the only configured library)\n", uikit.SymArrow)
		return ref, nil
	}

	if jsonMode {
		return zot.LibraryRef{}, fmt.Errorf("--library is required (values: personal, shared) — both libraries are configured; pass --library explicitly in --json mode")
	}

	picked, err := defaultLibraryPrompter([]zot.LibraryScope{zot.LibPersonal, zot.LibShared})
	if err != nil {
		return zot.LibraryRef{}, fmt.Errorf("library prompt: %w", err)
	}
	return resolveScopeWithProbe(ctx, cfg, picked)
}

// resolveScopeWithProbe wraps cfg.ResolveWithProbe with the standard
// API-backed group probe used everywhere in zot cli.
func resolveScopeWithProbe(ctx context.Context, cfg *zot.Config, scope zot.LibraryScope) (zot.LibraryRef, error) {
	probe := func() ([]zot.GroupRef, error) {
		tmp, err := api.New(cfg, api.WithLibrary(zot.LibraryRef{
			Scope:   zot.LibPersonal,
			APIPath: "users/" + cfg.UserID,
		}))
		if err != nil {
			return nil, err
		}
		return tmp.ListGroups(ctx)
	}
	return cfg.ResolveWithProbe(scope, probe)
}

// outputScoped is the zot-cli wrapper around cmdutil.Output that surfaces
// the active library on every leaf's output.
//
//   - Human mode: prepends a one-line "Library: <scope> — <name>" header.
//   - JSON mode:  injects a top-level `"library": "<scope>"` key into the
//     result object. Existing keys win (we never clobber a Result that
//     already declared a library field). For the rare result whose JSON()
//     is not a JSON object (slice/scalar), we fall through to the unwrapped
//     cmdutil.Output rather than rewrap and break consumers.
//
// Leaves that don't carry library scope (setup, info, find, guide) keep
// using cmdutil.Output directly — outputScoped no-ops gracefully when the
// holder is absent or unresolved.
func outputScoped(ctx context.Context, cmd *cli.Command, r cmdutil.Result) {
	h := libraryHolderFromCtx(ctx)
	if h == nil || h.Resolved == nil {
		cmdutil.Output(cmd, r)
		return
	}
	ref := *h.Resolved

	if cmdutil.IsJSON(cmd) {
		raw, err := jsonMarshal(r.JSON())
		if err != nil {
			cmdutil.Output(cmd, r)
			return
		}
		merged, ok := injectLibraryKey(raw, string(ref.Scope))
		if !ok {
			// Not a JSON object (slice/scalar) or already has "library"
			// — preserve existing shape so consumers don't break.
			cmdutil.Output(cmd, r)
			return
		}
		// Pretty-print: re-encode through json.Indent so output matches
		// cmdutil.Output's formatting.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, merged, "", "  "); err != nil {
			_, _ = os.Stdout.Write(merged)
			_, _ = os.Stdout.Write([]byte{'\n'})
			return
		}
		_, _ = os.Stdout.Write(pretty.Bytes())
		_, _ = os.Stdout.Write([]byte{'\n'})
		return
	}

	name := ref.Name
	if name == "" {
		name = string(ref.Scope)
	}
	header := fmt.Sprintf("  %s Library: %s — %s\n",
		uikit.SymArrow,
		uikit.TUI.TextBlue().Render(string(ref.Scope)),
		uikit.TUI.Dim().Render(name),
	)
	_, _ = os.Stdout.WriteString(header)
	fmt.Print(r.Human())
}

// jsonMarshal is json.Marshal with HTML escaping disabled, matching
// cmdutil.Output's encoder settings so the same bytes flow downstream.
func jsonMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Encoder appends a trailing newline; trim so callers can splice cleanly.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// injectLibraryKey prepends `"library": "<scope>"` to a JSON object.
// Preserves the inner field order (which a map round-trip would lose).
//
// Returns (merged, true) on success, or (nil, false) when raw is not a
// JSON object (slice / scalar / null) or already carries a top-level
// `"library"` key — in both fallback cases the caller should emit the
// original bytes unchanged so downstream consumers stay stable.
func injectLibraryKey(raw []byte, scope string) ([]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return nil, false
	}
	// Fast existence check: the substring `"library"` paired with an
	// immediately-following colon. This is overly conservative (would
	// match a field whose value contains the literal `"library":`) but
	// safe: a false positive only means we don't inject, preserving
	// behavior. Real Result types either have `library` as a top-level
	// field (StatsResult) or no library mention at all.
	if bytes.Contains(trimmed, []byte(`"library":`)) || bytes.Contains(trimmed, []byte(`"library" :`)) {
		return nil, false
	}

	scopeBytes, err := json.Marshal(scope)
	if err != nil {
		return nil, false
	}

	body := bytes.TrimSpace(trimmed[1 : len(trimmed)-1])
	var out bytes.Buffer
	out.WriteByte('{')
	out.WriteString(`"library":`)
	out.Write(scopeBytes)
	if len(body) > 0 {
		out.WriteByte(',')
		out.Write(body)
	}
	out.WriteByte('}')
	return out.Bytes(), true
}
